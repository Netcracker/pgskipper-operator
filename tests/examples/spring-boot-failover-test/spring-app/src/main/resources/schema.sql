-- Schema for connection testing
-- This will be created automatically by Hibernate, but provided for reference

CREATE TABLE IF NOT EXISTS connection_tests (
    id BIGSERIAL PRIMARY KEY,
    message VARCHAR(500) NOT NULL,
    hostname VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index for faster queries
CREATE INDEX IF NOT EXISTS idx_connection_tests_created_at ON connection_tests(created_at);
