export type Context = {
  owner_id: string;
  space_id: string;
  space_name: string;
  repo_id: string;
  checkout: string;
};
export type SourceFile = { path: string; blob: string; content: string };
export type Revision = {
  id: string;
  base: string;
  content: string;
  provenance: string;
  author_id: string;
  state: string;
  verification: string;
  associations: Record<string, string[]>;
  source?: {
    commit: string;
    path: string;
    blob: string;
    files?: SourceFile[];
  };
};
export type Choice = {
  artifact_id: string;
  qualified: string;
  alias: string;
  revision_id: string;
  state: string;
  reason: string;
  overridable: boolean;
  overrides?: string;
  override_revision?: string;
};
export type ArtifactSummary = {
  id: string;
  kind: string;
  lifecycle: string;
  head_revision: string;
};
export type ArtifactDetail = {
  artifact: ArtifactSummary & {
    space_id: string;
    repo_id: string;
    revisions: Revision[];
  };
  choices: Choice[];
  actions: { action: string; state: string; reason: string }[];
};
export type Destination = { space_id: string; repo_id: string };
export type Proposal = {
  id: string;
  revision_id: string;
  state: string;
  content: string;
  sources: { artifact_id: string; revision_id: string }[];
  destination: Destination;
};
export type Inventory = {
  context: Context;
  artifacts: ArtifactSummary[];
  proposals: {
    id: string;
    revision_id: string;
    state: string;
    destination: Destination;
  }[];
};
export type Review = {
  proposal: Proposal;
  source_context: Context;
  destination_context: Context;
  sources: {
    artifact_id: string;
    kind: string;
    revision: Revision;
    choice?: Choice;
  }[];
  policies: {
    scope: string;
    id: string;
    action: string;
    allowed: boolean;
    epoch: number;
  }[];
  audience: string;
  placement: string;
  provenance: string;
  dependencies: SourceFile[];
  snapshot: string;
};
