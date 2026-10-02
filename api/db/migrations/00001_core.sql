-- Core data model (spec section 1). Length limits are counted in characters
-- and mirror api/internal/domain; the domain package also checks the rules
-- that CHECK constraints can't express (per-tag length, duplicate tags,
-- custom-field value types).

-- +goose Up

CREATE TABLE users (
    id           uuid PRIMARY KEY,
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 40),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE projects (
    id          uuid PRIMARY KEY,
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    metadata    jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(metadata) = 'object'),
    -- The number of the project's latest event. Every change bumps it with
    -- UPDATE ... SET seq = seq + 1, which also locks the project row.
    seq         bigint NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE task_status AS ENUM ('todo', 'in_progress', 'in_review', 'done');
CREATE TYPE task_priority AS ENUM ('low', 'medium', 'high', 'urgent');

CREATE TABLE tasks (
    id            uuid PRIMARY KEY,
    project_id    uuid NOT NULL REFERENCES projects (id),
    title         text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    status        task_status NOT NULL DEFAULT 'todo',
    priority      task_priority NOT NULL DEFAULT 'medium',
    description   text NOT NULL DEFAULT '' CHECK (char_length(description) <= 10000),
    tags          text[] NOT NULL DEFAULT '{}' CHECK (cardinality(tags) <= 20),
    custom_fields jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(custom_fields) = 'object'),
    assignees     uuid[] NOT NULL DEFAULT '{}',
    -- The project seq of this task's last change; clients merge page loads
    -- with live events by comparing against it.
    updated_seq   bigint NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    -- Lets dependencies and comments enforce "same project" with composite
    -- FKs. It is also the index for paging a project's tasks by id.
    UNIQUE (project_id, id)
);

-- Each row means "task_id depends on depends_on_id".
CREATE TABLE task_dependencies (
    project_id    uuid NOT NULL,
    task_id       uuid NOT NULL,
    depends_on_id uuid NOT NULL,
    PRIMARY KEY (task_id, depends_on_id),
    FOREIGN KEY (project_id, task_id) REFERENCES tasks (project_id, id) ON DELETE CASCADE,
    FOREIGN KEY (project_id, depends_on_id) REFERENCES tasks (project_id, id) ON DELETE CASCADE,
    CHECK (task_id <> depends_on_id)
);

-- "Who depends on this task": the Done rule's reverse lookups and the delete
-- cascade's list of unblocked tasks.
CREATE INDEX task_dependencies_depends_on_id_idx ON task_dependencies (depends_on_id);

-- Comments are add-only. They go when their task is deleted.
CREATE TABLE comments (
    id         uuid PRIMARY KEY,
    task_id    uuid NOT NULL,
    project_id uuid NOT NULL,
    author_id  uuid NOT NULL REFERENCES users (id),
    content    text NOT NULL CHECK (char_length(content) BETWEEN 1 AND 5000),
    -- The project seq when the comment was posted; it sets the thread order.
    seq        bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (project_id, task_id) REFERENCES tasks (project_id, id) ON DELETE CASCADE
);

CREATE INDEX comments_task_id_seq_idx ON comments (task_id, seq);

-- The append-only change log. One row per accepted change, numbered by the
-- project's seq with no gaps.
CREATE TABLE events (
    project_id uuid NOT NULL REFERENCES projects (id),
    seq        bigint NOT NULL CHECK (seq > 0),
    change_id  uuid NOT NULL UNIQUE,
    type       text NOT NULL,
    entity_id  uuid NOT NULL,
    payload    jsonb NOT NULL,
    actor_id   uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, seq)
);

-- +goose StatementBegin
CREATE FUNCTION events_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'events is append-only: % is not allowed', TG_OP;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER events_append_only
    BEFORE UPDATE OR DELETE ON events
    FOR EACH STATEMENT EXECUTE FUNCTION events_append_only();

-- +goose Down

DROP TABLE events;
DROP FUNCTION events_append_only();
DROP TABLE comments;
DROP TABLE task_dependencies;
DROP TABLE tasks;
DROP TYPE task_priority;
DROP TYPE task_status;
DROP TABLE projects;
DROP TABLE users;
