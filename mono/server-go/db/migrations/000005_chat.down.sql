-- ============================================================================
-- RTVortex Database Schema — Migration 005 (Down): Drop Chat Tables
-- ============================================================================

DROP TRIGGER IF EXISTS trg_chat_sessions_updated_at ON chat_sessions;
DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS chat_sessions;

DELETE FROM schema_info WHERE version = 6;
