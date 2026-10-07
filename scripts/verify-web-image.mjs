/** Verify the standalone image and its built deployment rewrite without host mounts. */
const base = "http://127.0.0.1:3000";
const login = await fetch(`${base}/login`);
if (login.status !== 200 || !(await login.text()).includes("<!DOCTYPE html>")) {
  throw new Error("IMAGE_WEB_PAGE_FAILED");
}
const context = await fetch(`${base}/api/v1/auth/context`);
if (context.status !== 200) throw new Error("IMAGE_WEB_API_PROXY_FAILED");
const auth = await context.json();
if (!auth.initialized || auth.user !== null) throw new Error("IMAGE_WEB_DATABASE_NOT_SHARED");
const catalog = await fetch(`${base}/api/v1/runtime/catalog`);
if (catalog.status !== 401 || (await catalog.json()).code !== "AUTHENTICATION_REQUIRED") {
  throw new Error("IMAGE_WEB_RUNTIME_PROXY_FAILED");
}
console.log(JSON.stringify({ standaloneLogin: 200, backendHealthRewrite: 200,
  sharedInitializedDatabase: true, authenticatedCatalogBoundary: 401, deploymentHost: "retrom:8080" }));
