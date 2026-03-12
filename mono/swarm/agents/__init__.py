"""Swarm agent roles — each module implements one agent persona."""

from .architect import ArchitectAgent
from .junior_dev import JuniorDevAgent
from .orchestrator import OrchestratorAgent
from .qa import QAAgent
from .senior_dev import SeniorDevAgent

__all__ = [
    "ArchitectAgent",
    "JuniorDevAgent",
    "OrchestratorAgent",
    "QAAgent",
    "SeniorDevAgent",
]
