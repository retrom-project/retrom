export function violation(rule, file, line, message, chain) {
  return { rule, file, line, symbol: file, message, dependencyChain: chain };
}

export function dependencyPaths(origin, known, options = {}) {
  const queue = [[origin]];
  const seen = new Set([origin]);
  const result = [];
  for (let index = 0; index < queue.length; index += 1) {
    const chain = queue[index];
    const source = known.get(chain.at(-1));
    if (!source || (chain.length > 1 && options.stop?.(source))) {
      continue;
    }
    for (const edge of source.imports) {
      if ((options.runtime && edge.typeOnly) || !known.has(edge.resolved) || seen.has(edge.resolved)) {
        continue;
      }
      const next = [...chain, edge.resolved];
      seen.add(edge.resolved);
      queue.push(next);
      result.push(next);
    }
  }
  return result;
}

