// Read-only diagnostics: never mutate the store or expose resource paths/URLs.
export async function contentStoreSnapshot(page) {
  const events = (await Promise.all(page.frames().map(frame => frame.evaluate(() => globalThis.__retromContentStoreEvents ?? [])))).flat();
  const metrics = await Promise.all(page.frames().map(frame => frame.evaluate(() => globalThis.__retromContentIOMetrics ?? null)));
  const snapshot = await page.evaluate(async () => {
    const name = "retrom-content-io-v1";
    if (!(await indexedDB.databases()).some(database => database.name === name)) return {objects: [], generations: [], blocks: []};
    const db = await new Promise((resolve, reject) => {
      const request = indexedDB.open(name);
      request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error);
    });
    try {
      const values = await new Promise((resolve, reject) => {
        const names = ["objects", "generations", "blocks"], values = {};
        const tx = db.transaction(names, "readonly");
        for (const store of names) {
          const request = tx.objectStore(store).getAll();
          request.onsuccess = () => {values[store] = request.result;};
        }
        tx.oncomplete = () => resolve(values); tx.onerror = tx.onabort = () => reject(tx.error);
      });
      const cache = await caches.has(name) ? await caches.open(name) : null;
      const cacheBlocks = (cache ? await cache.keys() : []).map(request => new URL(request.url).pathname).filter(path => path.startsWith("/__retrom_content_io_v1__/block/"));
      return {cacheBlocks,
        objects: values.objects.map(item => ({key: item.key, generation: item.generation, backend: item.backend,
          state: item.state, sizeBytes: item.sizeBytes, committedBytes: item.committedBytes, revision: item.revision})),
        generations: values.generations.map(item => ({objectKey: item.objectKey, generation: item.generation,
          backend: item.backend, state: item.state, committedBytes: item.committedBytes, revision: item.revision,
          retiredAtMs: item.retiredAtMs})),
        blocks: values.blocks.map(item => ({objectKey: item.objectKey, generation: item.generation, index: item.index,
          backend: item.backend, length: item.length, commitRevision: item.commitRevision, provenance: item.provenance})),
      };
    } finally {db.close();}
  });
  return {...snapshot, events, metrics};
}
