"""MCP integration tools — @tool decorated functions for external service calls.

These tools let agents invoke connected external services (Slack, MS365,
Gmail, Discord) through the Go MCP service layer.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from ..sdk.tool import tool

logger = logging.getLogger(__name__)

_go_client = None
_user_id: str = ""
_org_id: str = ""


def init_mcp_tools(
    go_client: Any = None,
    user_id: str = "",
    org_id: str = "",
) -> None:
    global _go_client, _user_id, _org_id
    _go_client = go_client
    _user_id = user_id
    _org_id = org_id


def _get_go_client() -> Any:
    if _go_client is None:
        raise RuntimeError("MCP tools not initialised — call init_mcp_tools() first")
    return _go_client


@tool(description=(
    "Call an external service integration (Slack, MS365, Gmail, Discord). "
    "Requires a connected account. Use mcp_list_tools first to discover "
    "available providers and actions."
))
async def mcp_call(
    provider: str,
    action: str,
    params: str = "{}",
    agent_id: str = "",
    task_id: str = "",
) -> str:
    """Execute an action on an external connected service.

    Args:
        provider: The service provider name (slack, ms365, gmail, discord).
        action: The action to perform (e.g. send_message, list_channels).
        params: JSON string of action parameters.
        agent_id: The calling agent's identifier.
        task_id: The current task identifier.

    Returns:
        JSON string with the action result or error.
    """
    client = _get_go_client()
    try:
        parsed_params = json.loads(params) if isinstance(params, str) else params
    except json.JSONDecodeError:
        return json.dumps({"success": False, "error": f"Invalid JSON params: {params[:200]}"})

    try:
        result = await client.mcp_call(
            provider=provider,
            action=action,
            params=parsed_params,
            user_id=_user_id,
            org_id=_org_id,
            agent_id=agent_id,
            task_id=task_id,
        )
        return json.dumps(result, indent=2)
    except Exception as e:
        logger.error("mcp_call failed: %s", e)
        return json.dumps({"success": False, "error": str(e)})


@tool(description=(
    "List all connected MCP integration providers and their available "
    "actions. Use this to discover what external services are available "
    "before calling mcp_call."
))
async def mcp_list_tools() -> str:
    """List all available MCP providers and their actions.

    Returns:
        JSON string listing providers with their action definitions.
    """
    client = _get_go_client()
    try:
        result = await client.mcp_list_providers()
        return json.dumps(result, indent=2)
    except Exception as e:
        logger.error("mcp_list_tools failed: %s", e)
        return json.dumps({"error": str(e)})


@tool(description=(
    "List all active MCP connections for the current user. Shows which "
    "external services are connected and their status."
))
async def mcp_list_connections() -> str:
    """List active MCP connections for the current user.

    Returns:
        JSON string listing connected services with status information.
    """
    client = _get_go_client()
    try:
        result = await client.mcp_list_connections()
        return json.dumps(result, indent=2)
    except Exception as e:
        logger.error("mcp_list_connections failed: %s", e)
        return json.dumps({"error": str(e)})


@tool(description=(
    "Get detailed information about a specific MCP provider action, "
    "including required parameters, optional parameters, and whether "
    "consent is required."
))
async def mcp_describe_action(provider: str, action: str) -> str:
    """Describe a specific action for an MCP provider.

    Args:
        provider: The service provider name (slack, ms365, gmail, discord).
        action: The action name to describe.

    Returns:
        JSON string with the action definition including parameters.
    """
    client = _get_go_client()
    try:
        result = await client.mcp_describe_action(provider=provider, action=action)
        return json.dumps(result, indent=2)
    except Exception as e:
        logger.error("mcp_describe_action failed: %s", e)
        return json.dumps({"error": str(e)})


MCP_TOOLS = [mcp_call, mcp_list_tools, mcp_list_connections, mcp_describe_action]
