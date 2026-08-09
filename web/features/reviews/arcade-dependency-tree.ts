export type ArcadeParentAttachment = {
  attachmentId: string;
  machine: string;
  expectedLogicalName: string;
  originalFilename: string;
  state: "QUEUED" | "RUNNING" | "ACCEPTED" | "REJECTED" | "FAILED_RETRYABLE" | "CANCELLED";
  errorCode: string | null;
  jobId: string;
  diagnostics?: { archiveCode?: string; missingEntries?: string[]; mismatchedEntries?: string[] } | null;
};

export type ArcadeDependencyNode = {
  kind: "PARENT" | "BIOS_OR_BASE";
  machine: string;
  requiredBy: string | null;
  depth: number;
  expectedLogicalName: string;
  state: "MISSING" | "MISMATCH" | "SATISFIED_EXTERNAL" | "SATISFIED_BY_CONTENT" | "HASH_WARNING";
  requiredEntryCount: number;
  requiredEntries?: string[];
  canAttach: boolean;
  managementUrl?: string;
  attachment: ArcadeParentAttachment | null;
};

export type ArcadeDependencies = {
  machine: string;
  status: string;
  compatibilityCode: string;
  nodes: ArcadeDependencyNode[];
  activeAttachment: ArcadeParentAttachment | null;
};

export type ArcadeDependencyRow = { node: ArcadeDependencyNode; level: number };

function compareNodes(left: ArcadeDependencyNode, right: ArcadeDependencyNode) {
  if (left.depth !== right.depth) return left.depth - right.depth;
  if (left.kind !== right.kind) return left.kind === "PARENT" ? -1 : 1;
  return left.machine.localeCompare(right.machine, "en");
}

// The server owns dependency discovery. This function only turns its explicit
// requiredBy edges into a stable visual and keyboard order.
export function buildArcadeDependencyRows(value: ArcadeDependencies): ArcadeDependencyRow[] {
  const children = new Map<string, ArcadeDependencyNode[]>();
  for (const node of value.nodes) {
    const parent = node.requiredBy ?? value.machine;
    children.set(parent, [...(children.get(parent) ?? []), node]);
  }
  for (const items of children.values()) items.sort(compareNodes);

  const rows: ArcadeDependencyRow[] = [];
  const visited = new Set<string>();
  const visit = (machine: string, level: number) => {
    for (const node of children.get(machine) ?? []) {
      const key = `${node.kind}\u0000${node.machine}`;
      if (visited.has(key)) continue;
      visited.add(key);
      rows.push({ node, level });
      visit(node.machine, level + 1);
    }
  };
  visit(value.machine, 1);

  // Malformed/orphaned input remains visible for diagnosis instead of being
  // silently dropped. Its declared depth supplies a deterministic fallback.
  for (const node of [...value.nodes].sort(compareNodes)) {
    const key = `${node.kind}\u0000${node.machine}`;
    if (!visited.has(key)) rows.push({ node, level: Math.max(1, node.depth) });
  }
  return rows;
}
