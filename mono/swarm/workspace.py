"""Virtual workspace — in-memory file cache + changeset backed by VCS API.

No repository clone required.  Reads go through the Go server's VCS proxy
(which delegates to GitHub/GitLab/Bitbucket APIs).  Writes are held in
memory and converted to a changeset at the end for PR creation.

Thread-safety: all mutations acquire ``_lock`` (an :class:`asyncio.Lock`).
Since all swarm agents in a task share the same event loop, this serialises
concurrent writes from parallel agents while allowing concurrent reads of
the immutable VCS API.
"""

from __future__ import annotations

import asyncio
import difflib
import logging
from dataclasses import dataclass, field
from enum import Enum
from typing import Any

logger = logging.getLogger(__name__)


class ChangeType(str, Enum):
    MODIFIED = "modified"
    ADDED = "added"
    DELETED = "deleted"


@dataclass
class FileChange:
    """Tracks a pending change to a single file."""

    change_type: ChangeType
    original: str  # empty for new files
    proposed: str  # empty for deletions


class VirtualWorkspace:
    """In-memory workspace backed by Go VCS proxy for reads.

    Usage::

        ws = VirtualWorkspace(task_id="...", repo_id="...", go_client=go_client)
        content = await ws.read_file("src/auth.py")
        await ws.edit_file("src/auth.py", "def login():", "def login(user):")
        changeset = ws.get_changeset()  # → list[dict] ready for PR creator
    """

    def __init__(self, task_id: str, repo_id: str, go_client: Any):
        self.task_id = task_id
        self.repo_id = repo_id
        self._go = go_client
        self._file_cache: dict[str, str] = {}
        self._changeset: dict[str, FileChange] = {}
        self._lock = asyncio.Lock()

    # ── Reads ────────────────────────────────────────────────────────────

    async def read_file(self, path: str) -> str:
        """Read a file. Returns from cache if available, else fetches via VCS API."""
        path = _normalize_path(path)
        if path in self._file_cache:
            return self._file_cache[path]

        content = await self._go.vcs_read_file(self.repo_id, path)
        self._file_cache[path] = content
        return content

    async def list_dir(self, path: str = "") -> list[dict[str, str]]:
        """List directory contents via VCS API.

        Returns a list of ``{"name": "...", "type": "file"|"dir"}`` entries.
        """
        path = _normalize_path(path)
        entries = await self._go.vcs_list_dir(self.repo_id, path)
        return entries

    async def search(self, query: str, top_k: int = 10) -> str:
        """Semantic search via the C++ engine (existing search_code flow)."""
        # Delegate to the existing engine search — this is already wired.
        from .tools.engine_tools import _get_engine, _cached_search
        import json
        engine = _get_engine()
        result = await _cached_search(engine, query=query, repo_id=self.repo_id, top_k=top_k)
        return json.dumps(result, indent=2)

    # ── Writes ───────────────────────────────────────────────────────────

    async def edit_file(self, path: str, old_str: str, new_str: str) -> str:
        """Apply a search-and-replace edit to a file.

        If the file hasn't been read yet, it's fetched first. The replacement
        is performed in memory and tracked in the changeset.

        Returns:
            A confirmation string with the edit summary.

        Raises:
            ValueError: If *old_str* is not found in the file content.
        """
        path = _normalize_path(path)
        async with self._lock:
            # Ensure we have the current content.
            if path not in self._file_cache:
                content = await self._go.vcs_read_file(self.repo_id, path)
                self._file_cache[path] = content

            current = self._file_cache[path]
            if old_str not in current:
                # Provide context to help the LLM fix the issue.
                raise ValueError(
                    f"String not found in {path}. "
                    f"The file is {len(current)} chars long. "
                    f"Make sure old_str exactly matches the existing content "
                    f"(including whitespace and indentation)."
                )

            # Apply replacement (first occurrence only).
            new_content = current.replace(old_str, new_str, 1)
            self._file_cache[path] = new_content

            # Track change.
            if path in self._changeset:
                # Already modified — update proposed, keep original.
                self._changeset[path].proposed = new_content
            else:
                self._changeset[path] = FileChange(
                    change_type=ChangeType.MODIFIED,
                    original=current,
                    proposed=new_content,
                )

        return f"Edited {path}: replaced {len(old_str)} chars with {len(new_str)} chars."

    async def create_file(self, path: str, content: str) -> str:
        """Create a new file in the workspace.

        Returns:
            A confirmation string.
        """
        path = _normalize_path(path)
        async with self._lock:
            self._file_cache[path] = content
            self._changeset[path] = FileChange(
                change_type=ChangeType.ADDED,
                original="",
                proposed=content,
            )
        return f"Created {path} ({len(content)} chars)."

    async def edit_or_create_file(
        self, path: str, old_str: str, new_str: str
    ) -> str:
        """Edit a file if it exists, otherwise create it with *new_str*.

        This eliminates the "file not found" dead-end that agents hit when
        they try ``edit_file`` on a file that doesn't exist yet.  If the
        file exists and *old_str* is found, a normal search-and-replace is
        performed.  If the file does not exist (VCS 404), it is created
        with *new_str* as its full content.

        Returns:
            A confirmation string describing the action taken.
        """
        path = _normalize_path(path)
        async with self._lock:
            # Try to fetch from cache or VCS.
            if path not in self._file_cache:
                try:
                    content = await self._go.vcs_read_file(self.repo_id, path)
                    self._file_cache[path] = content
                except Exception:
                    # File doesn't exist — create it.
                    self._file_cache[path] = new_str
                    self._changeset[path] = FileChange(
                        change_type=ChangeType.ADDED,
                        original="",
                        proposed=new_str,
                    )
                    return (
                        f"File {path} not found — created with "
                        f"{len(new_str)} chars."
                    )

            current = self._file_cache[path]
            if old_str not in current:
                raise ValueError(
                    f"String not found in {path}. "
                    f"The file is {len(current)} chars long. "
                    f"Make sure old_str exactly matches the existing content "
                    f"(including whitespace and indentation)."
                )

            new_content = current.replace(old_str, new_str, 1)
            self._file_cache[path] = new_content

            if path in self._changeset:
                self._changeset[path].proposed = new_content
            else:
                self._changeset[path] = FileChange(
                    change_type=ChangeType.MODIFIED,
                    original=current,
                    proposed=new_content,
                )
        return f"Edited {path}: replaced {len(old_str)} chars with {len(new_str)} chars."

    async def create_module(
        self, files: list[dict[str, str]]
    ) -> str:
        """Atomically create multiple files as a single module.

        Accepts a list of ``{"path": "...", "content": "..."}`` dicts and
        creates all of them in one operation under the workspace lock.  This
        prevents partial module creation when an agent needs to produce an
        entire package (e.g. 6 new files in ``internal/sandbox/``).

        Parent directories are implicit — the VCS layer (and Git) tracks
        files, not directories.

        .. note::

            Atomicity is at the **in-memory changeset** level, not the VCS
            level.  All files are added to the changeset under a single lock
            acquisition so that concurrent readers never see a half-written
            module.

        Returns:
            A summary string listing all created files.
        """
        if not files:
            return "No files provided."

        created: list[str] = []
        async with self._lock:
            for entry in files:
                path = _normalize_path(entry["path"])
                content = entry["content"]
                self._file_cache[path] = content
                self._changeset[path] = FileChange(
                    change_type=ChangeType.ADDED,
                    original="",
                    proposed=content,
                )
                created.append(path)

        summary = ", ".join(created)
        return f"Created {len(created)} files: {summary}"

    async def delete_file(self, path: str) -> str:
        """Mark a file for deletion.

        Returns:
            A confirmation string.
        """
        path = _normalize_path(path)
        async with self._lock:
            original = self._file_cache.get(path, "")
            if not original:
                try:
                    original = await self._go.vcs_read_file(self.repo_id, path)
                except Exception:
                    original = ""
            self._file_cache[path] = ""
            self._changeset[path] = FileChange(
                change_type=ChangeType.DELETED,
                original=original,
                proposed="",
            )
        return f"Deleted {path}."

    # ── Changeset extraction ─────────────────────────────────────────────

    def get_changeset(self) -> list[dict[str, str]]:
        """Return all pending changes as a list of diff dicts.

        Each dict has: file_path, change_type, original, proposed, unified_diff.
        This is the same format the Go PR creator expects.
        """
        diffs: list[dict[str, str]] = []
        for path, change in self._changeset.items():
            unified = _make_unified_diff(path, change.original, change.proposed)
            diffs.append({
                "file_path": path,
                "change_type": change.change_type.value,
                "original": change.original,
                "proposed": change.proposed,
                "unified_diff": unified,
            })
        return diffs

    def has_changes(self) -> bool:
        """Return True if there are any pending changes."""
        return bool(self._changeset)

    def changed_files(self) -> list[str]:
        """Return paths of all changed files."""
        return list(self._changeset.keys())

    def file_is_changed(self, path: str) -> bool:
        """Return True if the given file has pending changes."""
        return _normalize_path(path) in self._changeset

    def get_file_content(self, path: str) -> str | None:
        """Return cached content for a file, or None if not cached."""
        return self._file_cache.get(_normalize_path(path))


def _normalize_path(path: str) -> str:
    """Strip leading slashes and './' prefixes."""
    path = path.strip()
    while path.startswith("/"):
        path = path[1:]
    while path.startswith("./"):
        path = path[2:]
    return path


def _make_unified_diff(path: str, original: str, proposed: str) -> str:
    """Generate a unified diff from original and proposed content."""
    original_lines = original.splitlines(keepends=True)
    proposed_lines = proposed.splitlines(keepends=True)
    diff_lines = difflib.unified_diff(
        original_lines,
        proposed_lines,
        fromfile=f"a/{path}",
        tofile=f"b/{path}",
        lineterm="",
    )
    return "".join(diff_lines)
