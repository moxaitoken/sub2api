-- Only supplier configuration; no upstream schema changes.
CREATE TABLE supplier.group_access (
 upstream_instance_id uuid NOT NULL REFERENCES supplier.upstream_instances(id),
 group_id bigint NOT NULL CHECK(group_id>0),
 enabled boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(upstream_instance_id,group_id)
);
CREATE TABLE supplier.group_access_initializations (
 upstream_instance_id uuid PRIMARY KEY REFERENCES supplier.upstream_instances(id)
);
CREATE TABLE supplier.group_access_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 upstream_instance_id uuid NOT NULL REFERENCES supplier.upstream_instances(id),
 group_id bigint NOT NULL,
 enabled boolean NOT NULL,
 actor_id bigint NOT NULL,
 version bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
