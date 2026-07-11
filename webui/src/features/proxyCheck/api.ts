import { apiRequest } from "../../lib/api-client";
import type {
  FetchRequest,
  FetchResponse,
  ImportRequest,
  ImportResponse,
  ProxyCheckOptions,
  ProxyCheckTask,
  SourceListResponse,
} from "./types";

const sourcesBase = "/api/v1/proxy-sources";
const tasksBase = "/api/v1/proxy-check/tasks";
const importBase = "/api/v1/proxy-check/import";

/** GET /api/v1/proxy-sources — list built-in safe proxy sources. */
export async function listProxySources(): Promise<SourceListResponse> {
  return apiRequest<SourceListResponse>(sourcesBase);
}

/** POST /api/v1/proxy-sources/fetch — fetch proxies from a given source. */
export async function fetchProxySources(req: FetchRequest): Promise<FetchResponse> {
  return apiRequest<FetchResponse>(`${sourcesBase}/fetch`, {
    method: "POST",
    body: req as never,
  });
}

/** POST /api/v1/proxy-check/tasks — create a batch proxy-check task. */
export async function createProxyCheckTask(
  proxies: string[],
  profile: string,
  options?: ProxyCheckOptions,
): Promise<ProxyCheckTask> {
  return apiRequest<ProxyCheckTask>(tasksBase, {
    method: "POST",
    body: { proxies, profile, options } as never,
  });
}

/** GET /api/v1/proxy-check/tasks/{id} — poll a task for status and results. */
export async function getProxyCheckTask(id: string): Promise<ProxyCheckTask> {
  return apiRequest<ProxyCheckTask>(`${tasksBase}/${id}`);
}

/** POST /api/v1/proxy-check/import — import verified proxies into the node pool. */
export async function importProxies(proxies: string[]): Promise<ImportResponse> {
  const body: ImportRequest = {
    proxies,
    confirm_checked: true,
  };
  return apiRequest<ImportResponse>(importBase, {
    method: "POST",
    body: body as never,
  });
}
