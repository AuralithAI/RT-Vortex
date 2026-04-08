// ─── LLM Markdown Renderer ───────────────────────────────────────────────────
// Lightweight markdown renderer for LLM-generated content. Handles code blocks
// with syntax labels + copy buttons, headings, lists (ordered & unordered),
// blockquotes, horizontal rules, bold, italic, inline code, and links.
// Designed for both dark (orchestration hero) and light (discussion cards)
// backgrounds via the `variant` prop.
// ─────────────────────────────────────────────────────────────────────────────

"use client";

import { useMemo, useState } from "react";
import { Check, ClipboardCopy, Code2 } from "lucide-react";
import { cn } from "@/lib/utils";

// ── Types ───────────────────────────────────────────────────────────────────

interface MdBlock {
  type: "paragraph" | "code" | "heading" | "list" | "blockquote" | "hr";
  content: string;
  language?: string;
  level?: number;
  items?: string[];
  ordered?: boolean;
}

type Variant = "light" | "dark";

interface LLMMarkdownProps {
  /** The raw markdown / LLM text to render. */
  content: string;
  /** Visual variant — "dark" for glass-panel backgrounds, "light" for cards. */
  variant?: Variant;
  /** Additional CSS classes on the outer container. */
  className?: string;
}

// ── Parser ──────────────────────────────────────────────────────────────────

function parseMarkdown(text: string): MdBlock[] {
  const blocks: MdBlock[] = [];
  const lines = text.split("\n");
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    // ── Fenced code block ───────────────────────────────────────
    if (line.startsWith("```")) {
      const lang = line.slice(3).trim();
      const codeLines: string[] = [];
      i++;
      while (i < lines.length && !lines[i].startsWith("```")) {
        codeLines.push(lines[i]);
        i++;
      }
      blocks.push({
        type: "code",
        content: codeLines.join("\n"),
        language: lang || undefined,
      });
      i++; // skip closing ```
      continue;
    }

    // ── Heading ─────────────────────────────────────────────────
    const headingMatch = line.match(/^(#{1,6})\s+(.+)/);
    if (headingMatch) {
      blocks.push({
        type: "heading",
        level: headingMatch[1].length,
        content: headingMatch[2],
      });
      i++;
      continue;
    }

    // ── Horizontal rule ─────────────────────────────────────────
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(line.trim())) {
      blocks.push({ type: "hr", content: "" });
      i++;
      continue;
    }

    // ── Unordered list ──────────────────────────────────────────
    if (/^[\s]*[-*+]\s/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^[\s]*[-*+]\s/.test(lines[i])) {
        items.push(lines[i].replace(/^[\s]*[-*+]\s/, ""));
        i++;
      }
      blocks.push({ type: "list", content: "", items, ordered: false });
      continue;
    }

    // ── Ordered list ────────────────────────────────────────────
    if (/^[\s]*\d+\.\s/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^[\s]*\d+\.\s/.test(lines[i])) {
        items.push(lines[i].replace(/^[\s]*\d+\.\s/, ""));
        i++;
      }
      blocks.push({ type: "list", content: "", items, ordered: true });
      continue;
    }

    // ── Blockquote ──────────────────────────────────────────────
    if (line.startsWith(">")) {
      const quoteLines: string[] = [];
      while (i < lines.length && lines[i].startsWith(">")) {
        quoteLines.push(lines[i].replace(/^>\s?/, ""));
        i++;
      }
      blocks.push({ type: "blockquote", content: quoteLines.join("\n") });
      continue;
    }

    // ── Empty line — skip ───────────────────────────────────────
    if (line.trim() === "") {
      i++;
      continue;
    }

    // ── Paragraph ───────────────────────────────────────────────
    const paraLines: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !lines[i].startsWith("```") &&
      !lines[i].startsWith("#") &&
      !lines[i].startsWith(">") &&
      !/^[\s]*[-*+]\s/.test(lines[i]) &&
      !/^[\s]*\d+\.\s/.test(lines[i]) &&
      !/^(-{3,}|\*{3,}|_{3,})$/.test(lines[i].trim())
    ) {
      paraLines.push(lines[i]);
      i++;
    }
    blocks.push({ type: "paragraph", content: paraLines.join("\n") });
  }

  return blocks;
}

// ── Inline renderer ─────────────────────────────────────────────────────────
// Handles: **bold**, *italic*, `inline code`, [links](url)

function InlineMarkdown({
  text,
  variant = "light",
}: {
  text: string;
  variant?: Variant;
}) {
  const parts: React.ReactNode[] = [];
  let remaining = text;
  let key = 0;

  while (remaining.length > 0) {
    const boldMatch = remaining.match(/\*\*(.+?)\*\*/);
    const italicMatch = remaining.match(/(?<!\*)\*(?!\*)(.+?)(?<!\*)\*(?!\*)/);
    const codeMatch = remaining.match(/`([^`]+)`/);
    const linkMatch = remaining.match(/\[([^\]]+)\]\(([^)]+)\)/);

    const matches = [
      boldMatch && { type: "bold", match: boldMatch },
      italicMatch && { type: "italic", match: italicMatch },
      codeMatch && { type: "code", match: codeMatch },
      linkMatch && { type: "link", match: linkMatch },
    ].filter(Boolean) as { type: string; match: RegExpMatchArray }[];

    if (matches.length === 0) {
      parts.push(remaining);
      break;
    }

    matches.sort((a, b) => (a.match.index ?? 0) - (b.match.index ?? 0));
    const first = matches[0];
    const idx = first.match.index ?? 0;

    if (idx > 0) {
      parts.push(remaining.slice(0, idx));
    }

    switch (first.type) {
      case "bold":
        parts.push(
          <strong key={key++} className="font-semibold">
            {first.match[1]}
          </strong>,
        );
        break;
      case "italic":
        parts.push(
          <em key={key++} className="italic opacity-80">
            {first.match[1]}
          </em>,
        );
        break;
      case "code":
        parts.push(
          <code
            key={key++}
            className={cn(
              "px-1.5 py-0.5 rounded text-xs font-mono",
              variant === "dark"
                ? "bg-white/10 text-emerald-300"
                : "bg-muted text-emerald-600 dark:text-emerald-400",
            )}
          >
            {first.match[1]}
          </code>,
        );
        break;
      case "link":
        parts.push(
          <a
            key={key++}
            href={first.match[2]}
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-400 hover:underline"
          >
            {first.match[1]}
          </a>,
        );
        break;
    }

    remaining = remaining.slice(idx + first.match[0].length);
  }

  return <>{parts}</>;
}

// ── Code Block ──────────────────────────────────────────────────────────────

function LLMCodeBlock({
  code,
  language,
  variant = "light",
}: {
  code: string;
  language?: string;
  variant?: Variant;
}) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div
      className={cn(
        "relative group rounded-lg overflow-hidden border",
        variant === "dark"
          ? "border-white/10 bg-black/30"
          : "border-border bg-muted/50 dark:bg-zinc-950",
      )}
    >
      {/* Header */}
      <div
        className={cn(
          "flex items-center justify-between px-3 py-1.5 border-b",
          variant === "dark"
            ? "bg-white/5 border-white/10"
            : "bg-muted dark:bg-zinc-900 border-border",
        )}
      >
        <div className="flex items-center gap-1.5">
          <Code2 className="h-3 w-3 opacity-50" />
          <span className="text-[10px] font-mono uppercase tracking-wide opacity-50">
            {language || "code"}
          </span>
        </div>
        <button
          onClick={handleCopy}
          className="opacity-0 group-hover:opacity-100 transition-opacity"
          title="Copy code"
        >
          {copied ? (
            <Check className="h-3.5 w-3.5 text-emerald-400" />
          ) : (
            <ClipboardCopy className="h-3.5 w-3.5 opacity-50 hover:opacity-100" />
          )}
        </button>
      </div>

      {/* Code content */}
      <pre
        className={cn(
          "p-3 overflow-x-auto text-xs font-mono leading-relaxed",
          variant === "dark" ? "text-white/80" : "text-foreground/80",
        )}
      >
        <code>{code}</code>
      </pre>
    </div>
  );
}

// ── Block Renderer ──────────────────────────────────────────────────────────

function MdBlockRenderer({
  block,
  variant = "light",
}: {
  block: MdBlock;
  variant?: Variant;
}) {
  const textClass =
    variant === "dark"
      ? "text-white/80 leading-relaxed"
      : "text-foreground leading-relaxed";

  const mutedClass =
    variant === "dark" ? "text-white/50" : "text-muted-foreground";

  switch (block.type) {
    case "code":
      return (
        <LLMCodeBlock
          code={block.content}
          language={block.language}
          variant={variant}
        />
      );

    case "heading": {
      const Tag = `h${Math.min(block.level ?? 3, 6)}` as keyof React.JSX.IntrinsicElements;
      const sizes: Record<number, string> = {
        1: "text-base font-bold",
        2: "text-sm font-bold",
        3: "text-[13px] font-semibold",
        4: "text-xs font-semibold",
        5: "text-xs font-medium",
        6: "text-xs font-medium opacity-70",
      };
      return (
        <Tag className={cn(sizes[block.level ?? 3], textClass, "mt-3 mb-1")}>
          <InlineMarkdown text={block.content} variant={variant} />
        </Tag>
      );
    }

    case "list": {
      const ListTag = block.ordered ? "ol" : "ul";
      return (
        <ListTag
          className={cn(
            "space-y-1 text-[13px]",
            textClass,
            block.ordered ? "list-decimal list-inside" : "list-disc list-inside",
          )}
        >
          {block.items?.map((item, i) => (
            <li key={i}>
              <InlineMarkdown text={item} variant={variant} />
            </li>
          ))}
        </ListTag>
      );
    }

    case "blockquote":
      return (
        <blockquote
          className={cn(
            "border-l-2 pl-3 italic text-[13px]",
            variant === "dark"
              ? "border-white/20 text-white/60"
              : "border-muted-foreground/30 text-muted-foreground",
          )}
        >
          <InlineMarkdown text={block.content} variant={variant} />
        </blockquote>
      );

    case "hr":
      return (
        <hr
          className={cn(
            variant === "dark" ? "border-white/10" : "border-border",
          )}
        />
      );

    case "paragraph":
    default:
      return (
        <p className={cn("text-[13px]", textClass)}>
          <InlineMarkdown text={block.content} variant={variant} />
        </p>
      );
  }
}

// ── Main Component ──────────────────────────────────────────────────────────

export function LLMMarkdown({
  content,
  variant = "light",
  className,
}: LLMMarkdownProps) {
  const blocks = useMemo(() => parseMarkdown(content), [content]);

  return (
    <div className={cn("space-y-2", className)}>
      {blocks.map((block, i) => (
        <MdBlockRenderer key={i} block={block} variant={variant} />
      ))}
    </div>
  );
}

export default LLMMarkdown;
