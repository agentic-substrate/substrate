import type { Revision } from "./artifactTypes";

export function RevisionContent({ revision }: { revision: Revision }) {
  return (
    <div className="revision">
      <dl>
        <dt>Revision</dt>
        <dd>{revision.id}</dd>
        <dt>Base revision</dt>
        <dd>{revision.base || "Independent addition"}</dd>
        <dt>State</dt>
        <dd>{revision.state}</dd>
        <dt>Verification</dt>
        <dd>{revision.verification}</dd>
        <dt>Author</dt>
        <dd>{revision.author_id}</dd>
        <dt>Provenance</dt>
        <dd>{revision.provenance || "No provenance claim supplied"}</dd>
      </dl>
      <h4>Full revision content</h4>
      <pre>{revision.content}</pre>
      <h4>Applicability and associations</h4>
      <p>Repository scope applies within the authorized space.</p>
      {Object.entries(revision.associations || {}).length === 0 ? (
        <p>No narrower associations recorded.</p>
      ) : (
        <dl>
          {Object.entries(revision.associations).map(([field, values]) => (
            <div key={field}>
              <dt>{field}</dt>
              <dd>{values.join(", ") || "None"}</dd>
            </div>
          ))}
        </dl>
      )}
      {revision.source && (
        <>
          <h4>Immutable Git source</h4>
          <dl>
            <dt>Commit</dt>
            <dd>{revision.source.commit}</dd>
            <dt>Path</dt>
            <dd>{revision.source.path}</dd>
            <dt>Blob</dt>
            <dd>{revision.source.blob}</dd>
          </dl>
          <h4>Declared dependencies</h4>
          {revision.source.files?.length ? (
            revision.source.files.map((file) => (
              <div key={file.path}>
                <p>
                  Path: {file.path}. Blob: {file.blob}
                </p>
                <pre>{file.content}</pre>
              </div>
            ))
          ) : (
            <p>No dependencies declared.</p>
          )}
        </>
      )}
    </div>
  );
}
