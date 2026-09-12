# HTTP 5xx investigation

1. Confirm the alert window and affected `service`; compare request rate and 5xx rate rather than using a raw error count alone.
2. Group errors by route and status. For public routes, follow the ownership map from `live-api` to commerce, interaction, or identity-room.
3. Use `trace_id` from the JSON log to inspect the same request in Tempo and in both gateway and downstream logs.
4. Check deployment events inside the window and inspect the recorded Git revision. A nearby release is correlation, not sufficient proof of causation.
5. Check the owner's hard dependencies and database pool metrics. Record missing telemetry as an evidence gap.
6. Prefer a reversible route or revision rollback proposal only when pre/post-release evidence supports it; execution still requires approval.
