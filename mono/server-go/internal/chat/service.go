// Package chat provides the RAG (Retrieval-Augmented Generation) chat service.
//
// Architecture:
//
//	User question
//	     │
//	     ▼
//	Engine Search (C++ engine — semantic + lexical + graph)
//	     │  returns relevant code chunks (FREE — no LLM cost)
//	     ▼
//	Build Prompt (user question + retrieved context + conversation history)
//	     │
//	     ▼
//	LLM Synthesis (only the final answer generation uses LLM tokens)
//	     │  streamed via SSE
//	     ▼
//	Response with citations to specific files/lines
package chat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/llm"
	"github.com/AuralithAI/rtvortex-server/internal/model"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// ── Service ─────────────────────────────────────────────────────────────────

// Config holds configuration for the chat service.
type Config struct {
	// MaxContextChunks is how many code chunks to retrieve per question.
	MaxContextChunks int

	// MaxConversationHistory is how many past messages to include for context.
	MaxConversationHistory int

	// MaxResponseTokens caps the LLM response length.
	MaxResponseTokens int

	// Temperature for LLM generation (lower = more precise).
	Temperature float64
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxContextChunks:       10,
		MaxConversationHistory: 10,
		MaxResponseTokens:      4096,
		Temperature:            0.3,
	}
}

// Service orchestrates the RAG chat pipeline.
type Service struct {
	chatRepo     *store.ChatRepository
	engineClient *engine.Client
	llmRegistry  *llm.Registry
	cfg          Config
}

// NewService creates a chat service.
func NewService(
	chatRepo *store.ChatRepository,
	engineClient *engine.Client,
	llmRegistry *llm.Registry,
	cfg Config,
) *Service {
	return &Service{
		chatRepo:     chatRepo,
		engineClient: engineClient,
		llmRegistry:  llmRegistry,
		cfg:          cfg,
	}
}

// ── Chat Request / Response ─────────────────────────────────────────────────

// SendMessageRequest is the input for sending a chat message.
type SendMessageRequest struct {
	SessionID   uuid.UUID              `json:"session_id"`
	RepoID      uuid.UUID              `json:"repo_id"`
	UserID      uuid.UUID              `json:"-"`
	Content     string                 `json:"content"`
	Attachments []model.ChatAttachment `json:"attachments,omitempty"`
}

// StreamCallback is called for each streaming event during chat generation.
type StreamCallback func(event model.ChatStreamEvent)

// ── Core RAG Pipeline ───────────────────────────────────────────────────────

// SendMessage processes a user message through the RAG pipeline:
// 1. Persist user message
// 2. Search engine for relevant code context
// 3. Build prompt with context + conversation history
// 4. Stream LLM response
// 5. Persist assistant message with citations
func (s *Service) SendMessage(ctx context.Context, req SendMessageRequest, onEvent StreamCallback) (*model.ChatMessage, error) {
	log := slog.With("session_id", req.SessionID, "repo_id", req.RepoID)

	log.Info("[Chat] message received",
		"user_id", req.UserID,
		"content_len", len(req.Content),
		"attachments", len(req.Attachments),
	)

	// 1. Persist user message.
	userMsg := &model.ChatMessage{
		SessionID:   req.SessionID,
		Role:        model.ChatRoleUser,
		Content:     req.Content,
		Attachments: req.Attachments,
	}
	if err := s.chatRepo.CreateMessage(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("persist user message: %w", err)
	}

	// 2. Search engine for relevant code chunks.
	onEvent(model.ChatStreamEvent{
		Type:    "thinking",
		Phase:   "searching",
		Message: "Searching repository for relevant code...",
	})

	searchStart := time.Now()
	var citations []model.ChatCitation
	var contextChunks []engine.ContextChunk

	if s.engineClient != nil {
		searchResult, err := s.engineClient.Search(ctx, req.RepoID.String(), req.Content, nil, engine.SearchConfig{
			TopK:             uint32(s.cfg.MaxContextChunks),
			LexicalWeight:    0.3,
			VectorWeight:     0.7,
			GraphExpandDepth: 2,
		})
		if err != nil {
			log.Warn("engine search failed, proceeding without context", "error", err)
			onEvent(model.ChatStreamEvent{
				Type:    "thinking",
				Phase:   "searching",
				Message: "Repository index unavailable — answering from general knowledge",
			})
		} else {
			contextChunks = searchResult.Chunks
			for _, chunk := range contextChunks {
				citation := model.ChatCitation{
					FilePath:       chunk.FilePath,
					StartLine:      int(chunk.StartLine),
					EndLine:        int(chunk.EndLine),
					Content:        chunk.Content,
					Language:       chunk.Language,
					RelevanceScore: chunk.RelevanceScore,
					Symbols:        chunk.Symbols,
				}
				citations = append(citations, citation)

				// Send each citation as a streaming event so the UI can show
				// "Referenced: src/attention.cpp:42-80" in real-time.
				onEvent(model.ChatStreamEvent{
					Type:     "citation",
					Citation: &citation,
				})
			}

			// Compute total context size for logging
			totalContextChars := 0
			for _, chunk := range contextChunks {
				totalContextChars += len(chunk.Content)
			}

			log.Info("[Chat] engine search completed",
				"chunks_retrieved", len(contextChunks),
				"total_context_chars", totalContextChars,
				"est_tokens", totalContextChars/4,
				"search_time_ms", time.Since(searchStart).Milliseconds(),
				"top_k", s.cfg.MaxContextChunks,
			)

			// Log individual chunks at debug level
			for i, chunk := range contextChunks {
				log.Debug("[Chat] retrieved chunk",
					"index", i,
					"file", chunk.FilePath,
					"lines", fmt.Sprintf("%d-%d", chunk.StartLine, chunk.EndLine),
					"score", fmt.Sprintf("%.4f", chunk.RelevanceScore),
					"chars", len(chunk.Content),
					"symbols", len(chunk.Symbols),
				)
			}
		}
	}
	searchTimeMs := int(time.Since(searchStart).Milliseconds())

	onEvent(model.ChatStreamEvent{
		Type:    "thinking",
		Phase:   "retrieving",
		Message: fmt.Sprintf("Found %d relevant code sections", len(contextChunks)),
	})

	// 3. Build conversation history.
	history, err := s.chatRepo.GetRecentMessages(ctx, req.SessionID, s.cfg.MaxConversationHistory)
	if err != nil {
		log.Warn("failed to load conversation history", "error", err)
	}

	// 4. Build the LLM prompt.
	onEvent(model.ChatStreamEvent{
		Type:    "thinking",
		Phase:   "synthesizing",
		Message: "Generating response...",
	})

	messages := s.buildPrompt(req.Content, req.Attachments, contextChunks, history)

	// 5. Stream LLM response.
	provider, ok := s.llmRegistry.Primary()
	if !ok {
		errMsg := "No LLM provider configured"
		onEvent(model.ChatStreamEvent{Type: "error", Error: errMsg})
		return nil, fmt.Errorf("%s", errMsg)
	}

	var fullResponse strings.Builder
	var usage llm.Usage

	// Try streaming first, fall back to non-streaming.
	streamProvider, isStreaming := provider.(llm.StreamingProvider)
	if isStreaming {
		ch, err := streamProvider.StreamComplete(ctx, &llm.CompletionRequest{
			Messages:    messages,
			MaxTokens:   s.cfg.MaxResponseTokens,
			Temperature: s.cfg.Temperature,
		})
		if err != nil {
			onEvent(model.ChatStreamEvent{Type: "error", Error: err.Error()})
			return nil, fmt.Errorf("LLM stream error: %w", err)
		}

		for chunk := range ch {
			if chunk.Content != "" {
				fullResponse.WriteString(chunk.Content)
				onEvent(model.ChatStreamEvent{
					Type:    "delta",
					Content: chunk.Content,
				})
			}
			if chunk.Usage != nil {
				usage = *chunk.Usage
			}
		}
	} else {
		// Non-streaming fallback.
		resp, err := provider.Complete(ctx, &llm.CompletionRequest{
			Messages:    messages,
			MaxTokens:   s.cfg.MaxResponseTokens,
			Temperature: s.cfg.Temperature,
		})
		if err != nil {
			onEvent(model.ChatStreamEvent{Type: "error", Error: err.Error()})
			return nil, fmt.Errorf("LLM error: %w", err)
		}
		fullResponse.WriteString(resp.Content)
		usage = resp.Usage

		// Send the full response as a single delta.
		onEvent(model.ChatStreamEvent{
			Type:    "delta",
			Content: resp.Content,
		})
	}

	// 6. Persist assistant message.
	assistantMsg := &model.ChatMessage{
		SessionID:        req.SessionID,
		Role:             model.ChatRoleAssistant,
		Content:          fullResponse.String(),
		Citations:        citations,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		SearchTimeMs:     searchTimeMs,
		ChunksRetrieved:  len(contextChunks),
	}
	if err := s.chatRepo.CreateMessage(ctx, assistantMsg); err != nil {
		log.Error("failed to persist assistant message", "error", err)
	}

	// 7. Auto-generate session title from first exchange.
	s.maybeGenerateTitle(ctx, req.SessionID, req.Content)

	// Send done event.
	onEvent(model.ChatStreamEvent{
		Type:             "done",
		MessageID:        &assistantMsg.ID,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		SearchTimeMs:     searchTimeMs,
		ChunksRetrieved:  len(contextChunks),
	})

	return assistantMsg, nil
}

// ── Prompt Building ─────────────────────────────────────────────────────────

const chatSystemPrompt = `You are RTVortex, a repository-aware AI code assistant built by AuralithAI.

## Identity
- You are **RTVortex**, not ChatGPT, Claude, Gemini, or any other AI brand.
- You were built by **AuralithAI** as part of the RTVortex platform.
- Never reveal or discuss the underlying base model, its training data, or its knowledge cutoff.
- Never say things like "my training data goes up to…" or "I was trained by…".
- If asked who you are, say: "I'm RTVortex, an AI code assistant by AuralithAI."

## How You Work (RAG Architecture)
Your knowledge comes from a **live semantic index** of this specific repository, not from pre-trained data:
1. The user's question is sent to the RTVortex C++ engine which searches the repository's indexed code.
2. Relevant code chunks (files, functions, classes) are retrieved via hybrid vector + lexical search.
3. Those chunks are provided to you as context (shown below as "Retrieved Code Context").
4. You synthesize an answer grounded in that retrieved context.

This means your knowledge is **always up to date** with the latest indexed state of the repository.
You do NOT rely on memorized training data — you rely on the live code index.

## Response Guidelines
1. **Reference specific code**: Always cite file paths and line numbers when relevant.
   Format citations as: ` + "`" + `[file.cpp:42-80]` + "`" + `
2. **Be precise**: Use the retrieved code context to give accurate, specific answers.
3. **Explain concepts**: When asked about architecture or design, explain the "why" not just the "what".
4. **Code examples**: When suggesting changes, show complete code snippets with proper syntax.
5. **Admit uncertainty**: If the retrieved context doesn't fully answer the question, say:
   "The indexed code I retrieved doesn't cover this fully — you may want to check [relevant area]."
   Do NOT blame a "training cutoff" or "knowledge date".
6. **Stay grounded**: Only answer based on the retrieved code context. Do not hallucinate files or code that isn't in the context.

Format your response using Markdown with:
- Fenced code blocks with language identifiers
- Headers for organization
- Bullet points for lists
- Bold for emphasis on key concepts`

func (s *Service) buildPrompt(
	userQuestion string,
	attachments []model.ChatAttachment,
	chunks []engine.ContextChunk,
	history []*model.ChatMessage,
) []llm.Message {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: chatSystemPrompt},
	}

	// Add retrieved code context as a system message.
	if len(chunks) > 0 {
		var contextBuilder strings.Builder
		contextBuilder.WriteString("## Retrieved Code Context\n\n")
		contextBuilder.WriteString("The following code sections are relevant to the user's question:\n\n")

		for i, chunk := range chunks {
			if i >= s.cfg.MaxContextChunks {
				break
			}
			contextBuilder.WriteString(fmt.Sprintf("### %s (L%d–L%d, relevance: %.2f)\n",
				chunk.FilePath, chunk.StartLine, chunk.EndLine, chunk.RelevanceScore))
			if len(chunk.Symbols) > 0 {
				contextBuilder.WriteString(fmt.Sprintf("Symbols: %s\n", strings.Join(chunk.Symbols, ", ")))
			}
			contextBuilder.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", chunk.Language, chunk.Content))
		}

		messages = append(messages, llm.Message{
			Role:    llm.RoleSystem,
			Content: contextBuilder.String(),
		})
	}

	// Add conversation history (skip the current message).
	for _, msg := range history {
		if msg.Role == model.ChatRoleUser {
			messages = append(messages, llm.Message{
				Role: llm.RoleUser, Content: msg.Content,
			})
		} else if msg.Role == model.ChatRoleAssistant {
			messages = append(messages, llm.Message{
				Role: llm.RoleAssistant, Content: msg.Content,
			})
		}
	}

	// Build user message with attachments.
	var userContent strings.Builder
	userContent.WriteString(userQuestion)

	for _, att := range attachments {
		userContent.WriteString(fmt.Sprintf("\n\n--- Attached %s: %s ---\n", att.Type, att.Filename))
		if att.Language != "" {
			userContent.WriteString(fmt.Sprintf("```%s\n%s\n```", att.Language, att.Content))
		} else {
			userContent.WriteString(att.Content)
		}
	}

	messages = append(messages, llm.Message{
		Role:    llm.RoleUser,
		Content: userContent.String(),
	})

	return messages
}

// maybeGenerateTitle auto-generates a title for the session if it's still "New Chat".
func (s *Service) maybeGenerateTitle(ctx context.Context, sessionID uuid.UUID, firstQuestion string) {
	session, err := s.chatRepo.GetSession(ctx, sessionID)
	if err != nil || session.Title != "New Chat" {
		return
	}

	// Simple title: first 60 chars of the question.
	title := firstQuestion
	if len(title) > 60 {
		title = title[:57] + "..."
	}
	_ = s.chatRepo.UpdateSessionTitle(ctx, sessionID, title)
}
