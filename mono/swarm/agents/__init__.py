"""Swarm agent roles — each module implements one agent persona."""

from .architect import ArchitectAgent
from .docs import DocsAgent
from .junior_dev import JuniorDevAgent
from .ops import OpsAgent
from .orchestrator import OrchestratorAgent
from .qa import QAAgent
from .security import SecurityAgent
from .senior_dev import SeniorDevAgent
from .ui_ux import UIUXAgent

__all__ = [
    "ArchitectAgent",
    "DocsAgent",
    "JuniorDevAgent",
    "OpsAgent",
    "OrchestratorAgent",
    "QAAgent",
    "SecurityAgent",
    "SeniorDevAgent",
    "UIUXAgent",
]
