/** Placeholder: the band designer is built in a later packet. */
export function ReportDesigner(props: { ws: string; reportId: string | null; tabKey: string }) {
  return (
    <p className="p-4 text-fg-muted" data-report={props.reportId ?? ""}>
      The report designer is coming soon.
    </p>
  );
}
