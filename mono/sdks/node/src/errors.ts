/**
 * Exception hierarchy for RTVortex Node.js SDK.
 */

export class RTVortexError extends Error {
  public readonly statusCode?: number;
  public readonly body?: unknown;

  constructor(message: string, statusCode?: number, body?: unknown) {
    super(message);
    this.name = "RTVortexError";
    this.statusCode = statusCode;
    this.body = body;
  }
}

export class AuthenticationError extends RTVortexError {
  constructor(message: string, body?: unknown) {
    super(message, 401, body);
    this.name = "AuthenticationError";
  }
}

export class NotFoundError extends RTVortexError {
  constructor(message: string, body?: unknown) {
    super(message, 404, body);
    this.name = "NotFoundError";
  }
}

export class ValidationError extends RTVortexError {
  constructor(message: string, body?: unknown) {
    super(message, 422, body);
    this.name = "ValidationError";
  }
}

export class QuotaExceededError extends RTVortexError {
  constructor(message: string, statusCode: number, body?: unknown) {
    super(message, statusCode, body);
    this.name = "QuotaExceededError";
  }
}

export class ServerError extends RTVortexError {
  constructor(message: string, statusCode: number, body?: unknown) {
    super(message, statusCode, body);
    this.name = "ServerError";
  }
}

/**
 * Map an HTTP response status to the appropriate SDK error.
 */
export async function throwForStatus(response: Response): Promise<void> {
  if (response.ok) return;

  let body: unknown;
  let msg: string;
  try {
    body = await response.json();
    msg =
      typeof body === "object" && body !== null && "error" in body
        ? String((body as Record<string, unknown>).error)
        : response.statusText;
  } catch {
    body = await response.text().catch(() => "");
    msg = typeof body === "string" ? body : response.statusText;
  }

  const code = response.status;

  switch (code) {
    case 401:
      throw new AuthenticationError(msg, body);
    case 404:
      throw new NotFoundError(msg, body);
    case 422:
      throw new ValidationError(msg, body);
    case 403:
    case 429:
      throw new QuotaExceededError(msg, code, body);
    default:
      if (code >= 500) throw new ServerError(msg, code, body);
      throw new RTVortexError(msg, code, body);
  }
}
