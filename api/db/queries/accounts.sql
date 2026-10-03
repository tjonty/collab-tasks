-- Users and projects: the two writes that don't go through the change
-- pipeline.

-- name: InsertUser :one
INSERT INTO users (id, display_name) VALUES ($1, $2)
RETURNING id, display_name;

-- name: UserExists :one
SELECT EXISTS (SELECT 1 FROM users WHERE id = $1);

-- name: InsertProject :exec
-- A retried create (same id) leaves the existing project as it is.
INSERT INTO projects (id, name, description, metadata)
VALUES (@id, @name, @description, @metadata)
ON CONFLICT (id) DO NOTHING;
