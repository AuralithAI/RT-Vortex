"""Multimodal asset tools — @tool decorated functions for ingesting and
searching images, audio, PDFs, and web pages.

These tools let swarm agents upload and embed non-code assets so that the
LLM can find relevant screenshots, design docs, voice recordings, and
external references during code review and development tasks.
"""

from __future__ import annotations

import base64
import json
import logging
import mimetypes
from pathlib import Path
from typing import Any

from ..sdk.tool import tool

logger = logging.getLogger(__name__)

# Module-level clients — set via init_multimodal_tools().
_go_client = None
_engine_client = None


def init_multimodal_tools(
    go_client: Any = None,
    engine_client: Any = None,
) -> None:
    """Set the HTTP/gRPC clients for multimodal tools.  Call once at startup."""
    global _go_client, _engine_client
    _go_client = go_client
    _engine_client = engine_client


# ── Ingestion tools ─────────────────────────────────────────────────────────


@tool(description=(
    "Upload an image file (PNG, JPEG, WEBP, SVG) to be embedded and made "
    "searchable alongside the codebase.  Use this for screenshots, "
    "architecture diagrams, UI mockups, or any visual reference."
))
async def upload_image(
    file_path: str,
    repo_id: str,
    description: str = "",
) -> str:
    """Upload an image and embed it for semantic search.

    Args:
        file_path: Absolute path to the image file in the workspace.
        repo_id: Repository identifier.
        description: Optional text description of the image.

    Returns:
        JSON with asset ID and processing status.
    """
    return await _upload_file(file_path, repo_id, "image", description)


@tool(description=(
    "Upload an audio file (WAV, MP3, OGG, FLAC) to be embedded and made "
    "searchable alongside the codebase.  Use this for voice recordings, "
    "meeting notes, or audio assets."
))
async def upload_audio(
    file_path: str,
    repo_id: str,
    description: str = "",
) -> str:
    """Upload an audio file and embed it for semantic search.

    Args:
        file_path: Absolute path to the audio file in the workspace.
        repo_id: Repository identifier.
        description: Optional text description of the audio.

    Returns:
        JSON with asset ID and processing status.
    """
    return await _upload_file(file_path, repo_id, "audio", description)


@tool(description=(
    "Upload a PDF document to be embedded and made searchable alongside "
    "the codebase.  Text is extracted automatically.  Use this for "
    "specifications, design documents, or any PDF reference material."
))
async def upload_pdf(
    file_path: str,
    repo_id: str,
    description: str = "",
) -> str:
    """Upload a PDF document and embed it for semantic search.

    Args:
        file_path: Absolute path to the PDF file in the workspace.
        repo_id: Repository identifier.
        description: Optional text description of the document.

    Returns:
        JSON with asset ID and processing status.
    """
    return await _upload_file(file_path, repo_id, "pdf", description)


@tool(description=(
    "Fetch a web page URL and embed its content for semantic search "
    "alongside the codebase.  Use this to index external documentation, "
    "API references, or blog posts relevant to the project."
))
async def ingest_url(
    url: str,
    repo_id: str,
) -> str:
    """Fetch and embed a web page for semantic search.

    Args:
        url: The full URL of the web page to fetch and embed.
        repo_id: Repository identifier.

    Returns:
        JSON with asset ID and processing status.
    """
    if not _go_client:
        return json.dumps({"error": "Go client not available — cannot ingest URL"})

    try:
        result = await _go_client.post(
            f"/api/v1/repos/{repo_id}/assets/ingest-url",
            json={"url": url},
        )
        return json.dumps(result, indent=2)
    except Exception as e:
        logger.warning("URL ingestion failed: %s", e)
        return json.dumps({"error": str(e)})


# ── Search tools ────────────────────────────────────────────────────────────


@tool(description=(
    "Search for assets (images, audio, PDFs, documents) that have been "
    "uploaded and embedded for this repository.  Returns matching assets "
    "with metadata including file names, types, and processing status."
))
async def search_assets(
    repo_id: str,
) -> str:
    """List all indexed assets for a repository.

    Args:
        repo_id: Repository identifier.

    Returns:
        JSON array of assets with IDs, types, file names, and statuses.
    """
    if not _go_client:
        return json.dumps({"error": "Go client not available — cannot list assets"})

    try:
        result = await _go_client.get(f"/api/v1/repos/{repo_id}/assets")
        return json.dumps(result, indent=2)
    except Exception as e:
        logger.warning("Asset listing failed: %s", e)
        return json.dumps({"error": str(e)})


@tool(description=(
    "Delete an uploaded asset by its ID.  This removes the asset and its "
    "embeddings from the search index."
))
async def delete_asset(
    asset_id: str,
    repo_id: str,
) -> str:
    """Delete an uploaded asset.

    Args:
        asset_id: The UUID of the asset to delete.
        repo_id: Repository identifier.

    Returns:
        JSON with deletion status.
    """
    if not _go_client:
        return json.dumps({"error": "Go client not available — cannot delete asset"})

    try:
        result = await _go_client.delete(f"/api/v1/repos/{repo_id}/assets/{asset_id}")
        return json.dumps(result, indent=2)
    except Exception as e:
        logger.warning("Asset deletion failed: %s", e)
        return json.dumps({"error": str(e)})


# ── Internal helpers ────────────────────────────────────────────────────────


async def _upload_file(
    file_path: str,
    repo_id: str,
    expected_type: str,
    description: str = "",
) -> str:
    """Upload a file to the Go server's asset upload endpoint."""
    if not _go_client:
        return json.dumps({"error": "Go client not available — cannot upload asset"})

    path = Path(file_path)
    if not path.is_file():
        return json.dumps({"error": f"File not found: {file_path}"})

    # Read file and detect MIME type.
    data = path.read_bytes()
    mime_type, _ = mimetypes.guess_type(str(path))
    if mime_type is None:
        mime_type = "application/octet-stream"

    # Validate expected type vs detected MIME.
    if expected_type == "image" and not mime_type.startswith("image/"):
        return json.dumps({
            "error": f"Expected image but detected MIME type: {mime_type}",
            "file": str(path),
        })
    if expected_type == "audio" and not mime_type.startswith("audio/"):
        return json.dumps({
            "error": f"Expected audio but detected MIME type: {mime_type}",
            "file": str(path),
        })
    if expected_type == "pdf" and mime_type != "application/pdf":
        return json.dumps({
            "error": f"Expected PDF but detected MIME type: {mime_type}",
            "file": str(path),
        })

    # Upload via multipart form data to Go server.
    try:
        result = await _go_client.upload_file(
            f"/api/v1/repos/{repo_id}/assets/upload",
            file_name=path.name,
            file_data=data,
            mime_type=mime_type,
        )
        return json.dumps(result, indent=2)
    except AttributeError:
        # If go_client doesn't support upload_file, fall back to engine client.
        pass
    except Exception as e:
        logger.warning("File upload via Go server failed: %s", e)
        return json.dumps({"error": str(e)})

    # Fallback: send directly to engine via gRPC.
    if _engine_client:
        try:
            result = await _engine_client.ingest_asset(
                repo_id=repo_id,
                source_url=f"file://{path}",
                content=description or path.name,
                asset_type=expected_type,
                metadata={"file_name": path.name, "mime_type": mime_type},
            )
            return json.dumps(result, indent=2)
        except Exception as e:
            logger.warning("Engine ingest fallback failed: %s", e)
            return json.dumps({"error": str(e)})

    return json.dumps({"error": "No upload client available"})
