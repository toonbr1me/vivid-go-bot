# Graphify snapshot

This directory holds a compact, reviewable snapshot of the code graph. It is
not part of the application runtime.

Tracked files:

- `GRAPH_REPORT.md` — high-level map of packages and relationships;
- `FLOW_ANALYSIS.md` and `FLOW_CATALOG.md` — user-facing flows and their
  source anchors;
- `FLOW_MAP.html` — an offline, interactive flow map.

The raw AST, JSON exports, cache, and Graphify working files are deliberately
ignored. They are regenerated locally and would add tens of megabytes of
machine-specific data to every revision.

Refresh the tracked snapshot whenever a substantial flow changes, then review
the generated diff before committing it. Treat the graph as navigation and a
review aid, not as an authoritative description of runtime behaviour.
