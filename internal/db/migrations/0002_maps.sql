-- +goose Up

-- UUIDv7, matching the ids the app generates. Only used for rows created in SQL.
CREATE FUNCTION oms_uuid_v7() RETURNS uuid LANGUAGE sql VOLATILE AS $$
  SELECT encode(set_bit(set_bit(overlay(uuid_send(gen_random_uuid()) PLACING
    substring(int8send(floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint) FROM 3) FROM 1 FOR 6),
    52, 1), 53, 1), 'hex')::uuid
$$;

CREATE TABLE workflows (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         text NOT NULL,
    is_default   boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX workflows_workspace_id ON workflows (workspace_id);
CREATE UNIQUE INDEX workflows_one_default ON workflows (workspace_id) WHERE is_default;

CREATE TABLE statuses (
    id          uuid PRIMARY KEY,
    workflow_id uuid NOT NULL REFERENCES workflows (id) ON DELETE CASCADE,
    name        text NOT NULL,
    category    text NOT NULL CHECK (category IN ('todo', 'doing', 'done', 'blocked')),
    colour      text,
    rank        text COLLATE "C" NOT NULL,
    ext_name    text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, name),
    UNIQUE (workflow_id, rank)
);

-- A workflow with the standard four statuses. Every workspace starts with one
-- named "Default"; new workflows start from the same statuses.
-- +goose StatementBegin
CREATE FUNCTION oms_seed_workflow(ws uuid, wf_name text, make_default boolean) RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE wf uuid := oms_uuid_v7();
BEGIN
  INSERT INTO workflows (id, workspace_id, name, is_default) VALUES (wf, ws, wf_name, make_default);
  INSERT INTO statuses (id, workflow_id, name, category, colour, rank) VALUES
    (oms_uuid_v7(), wf, 'To do',       'todo',    '#8A8A93', 'a0'),
    (oms_uuid_v7(), wf, 'In progress', 'doing',   '#FCB131', 'a1'),
    (oms_uuid_v7(), wf, 'Blocked',     'blocked', '#BA3636', 'a2'),
    (oms_uuid_v7(), wf, 'Done',        'done',    '#549334', 'a3');
  RETURN wf;
END
$$;
-- +goose StatementEnd

SELECT oms_seed_workflow(id, 'Default', true) FROM workspaces;

CREATE TABLE maps (
    id           uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    workflow_id  uuid NOT NULL REFERENCES workflows (id),
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    deleted_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX maps_workspace_id ON maps (workspace_id);

CREATE TABLE nodes (
    id          uuid PRIMARY KEY,
    map_id      uuid NOT NULL REFERENCES maps (id) ON DELETE CASCADE,
    parent_id   uuid REFERENCES nodes (id) ON DELETE CASCADE,
    level       text NOT NULL CHECK (level IN ('objective', 'activity')),
    title       text NOT NULL,
    description text NOT NULL DEFAULT '',
    colour      text,
    rank        text COLLATE "C" NOT NULL,
    version     integer NOT NULL DEFAULT 0,
    ext_system  text,
    ext_key     text,
    ext_url     text,
    deleted_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (num_nulls(ext_system, ext_key) IN (0, 2))
);
CREATE UNIQUE INDEX nodes_sibling_rank ON nodes (map_id, parent_id, rank) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX nodes_ext ON nodes (map_id, ext_system, ext_key) WHERE ext_system IS NOT NULL;
CREATE INDEX nodes_parent_id ON nodes (parent_id);

-- An objective has no parent; an activity's parent is an objective on the same map.
-- +goose StatementBegin
CREATE FUNCTION oms_nodes_check_parent() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  parent_level text;
  parent_map   uuid;
BEGIN
  IF NEW.parent_id IS NOT NULL THEN
    SELECT level, map_id INTO parent_level, parent_map FROM nodes WHERE id = NEW.parent_id;
    IF parent_map IS DISTINCT FROM NEW.map_id THEN
      RAISE EXCEPTION 'node % has a parent on another map', NEW.id USING ERRCODE = 'check_violation';
    END IF;
  END IF;
  IF NEW.level = 'activity' AND parent_level IS DISTINCT FROM 'objective' THEN
    RAISE EXCEPTION 'an activity must sit under an objective' USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.level = 'objective' AND NEW.parent_id IS NOT NULL THEN
    RAISE EXCEPTION 'an objective has no parent' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER nodes_check_parent AFTER INSERT OR UPDATE OF parent_id, level, map_id ON nodes
    FOR EACH ROW EXECUTE FUNCTION oms_nodes_check_parent();

CREATE TABLE releases (
    id          uuid PRIMARY KEY,
    map_id      uuid NOT NULL REFERENCES maps (id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    target_date date,
    rank        text COLLATE "C" NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (map_id, rank)
);

CREATE TABLE steps (
    id             uuid PRIMARY KEY,
    workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key            text NOT NULL,
    title          text NOT NULL,
    description_md text NOT NULL DEFAULT '',
    status_id      uuid NOT NULL REFERENCES statuses (id) ON DELETE RESTRICT,
    estimate       numeric(6, 2),
    assignee_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    version        integer NOT NULL DEFAULT 0,
    ext_system     text,
    ext_key        text,
    ext_url        text,
    search         tsvector GENERATED ALWAYS AS (to_tsvector('simple', title || ' ' || description_md)) STORED,
    deleted_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, key),
    CHECK (num_nulls(ext_system, ext_key) IN (0, 2))
);
CREATE INDEX steps_search ON steps USING gin (search);
CREATE INDEX steps_status_id ON steps (status_id);
CREATE UNIQUE INDEX steps_ext ON steps (workspace_id, ext_system, ext_key) WHERE ext_system IS NOT NULL;

CREATE TABLE placements (
    id         uuid PRIMARY KEY,
    step_id   uuid NOT NULL REFERENCES steps (id) ON DELETE CASCADE,
    map_id     uuid NOT NULL REFERENCES maps (id) ON DELETE CASCADE,
    activity_id uuid NOT NULL REFERENCES nodes (id) ON DELETE CASCADE,
    release_id uuid REFERENCES releases (id) ON DELETE SET NULL,
    rank       text COLLATE "C" NOT NULL,
    UNIQUE (step_id, map_id)
);
CREATE UNIQUE INDEX placements_cell_rank ON placements (map_id, activity_id, release_id, rank) NULLS NOT DISTINCT;
CREATE INDEX placements_activity_id ON placements (activity_id);
CREATE INDEX placements_release_id ON placements (release_id);

-- +goose StatementBegin
CREATE FUNCTION oms_placements_check() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM nodes WHERE id = NEW.activity_id AND map_id = NEW.map_id AND level = 'activity') THEN
    RAISE EXCEPTION 'placement % must sit under an activity of map %', NEW.id, NEW.map_id USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.release_id IS NOT NULL
     AND NOT EXISTS (SELECT 1 FROM releases WHERE id = NEW.release_id AND map_id = NEW.map_id) THEN
    RAISE EXCEPTION 'placement % has a release from another map', NEW.id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER placements_check AFTER INSERT OR UPDATE OF map_id, activity_id, release_id ON placements
    FOR EACH ROW EXECUTE FUNCTION oms_placements_check();

CREATE TABLE events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    map_id       uuid REFERENCES maps (id) ON DELETE CASCADE,
    actor_id     uuid REFERENCES users (id) ON DELETE SET NULL,
    actor_kind   text NOT NULL CHECK (actor_kind IN ('user', 'token', 'system')),
    entity_type  text NOT NULL,
    entity_id    uuid NOT NULL,
    action       text NOT NULL,
    before       jsonb,
    after        jsonb,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_workspace ON events (workspace_id, id);
CREATE INDEX events_entity ON events (entity_type, entity_id, id);
CREATE INDEX events_map ON events (map_id, id);

-- +goose Down
DROP TABLE events;
DROP TABLE placements;
DROP FUNCTION oms_placements_check;
DROP TABLE steps;
DROP TABLE releases;
DROP TABLE nodes;
DROP FUNCTION oms_nodes_check_parent;
DROP TABLE maps;
DROP FUNCTION oms_seed_workflow;
DROP TABLE statuses;
DROP TABLE workflows;
DROP FUNCTION oms_uuid_v7;
