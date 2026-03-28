/**
 * @rtvortex/sdk — Official Node.js / TypeScript SDK for the RTVortex API.
 *
 * @example
 * ```ts
 * import { RTVortexClient } from "@rtvortex/sdk";
 *
 * const client = new RTVortexClient({ token: "your-token" });
 * const user = await client.me();
 * ```
 */

export { RTVortexClient } from "./client.js";
export type { RTVortexClientOptions } from "./client.js";

export {
  RTVortexError,
  AuthenticationError,
  NotFoundError,
  ValidationError,
  QuotaExceededError,
  ServerError,
} from "./errors.js";

export { parseSSEBlock, iterSSEEvents } from "./streaming.js";

export type {
  User,
  UserUpdateRequest,
  Org,
  OrgMember,
  Repo,
  Review,
  ReviewComment,
  ProgressEvent,
  IndexStatus,
  AdminStats,
  HealthStatus,
  PaginationOptions,
  PaginatedResponse,
  OrgListResponse,
  MemberListResponse,
  RepoListResponse,
  ReviewListResponse,
} from "./types.js";
