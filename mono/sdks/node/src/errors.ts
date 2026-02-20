/**
 * Custom error classes for AI-PR-Reviewer SDK
 */

/**
 * Base error class for AIPR SDK
 */
export class AIPRError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "AIPRError";
    Object.setPrototypeOf(this, AIPRError.prototype);
  }
}

/**
 * Error thrown when an API request fails
 */
export class AIPRAPIError extends AIPRError {
  /** HTTP status code */
  readonly statusCode: number | undefined;
  /** Response body */
  readonly responseBody: string | undefined;

  constructor(
    message: string,
    statusCode?: number,
    responseBody?: string
  ) {
    super(message);
    this.name = "AIPRAPIError";
    this.statusCode = statusCode;
    this.responseBody = responseBody;
    Object.setPrototypeOf(this, AIPRAPIError.prototype);
  }

  /**
   * Check if this is a client error (4xx)
   */
  get isClientError(): boolean {
    return this.statusCode !== undefined && this.statusCode >= 400 && this.statusCode < 500;
  }

  /**
   * Check if this is a server error (5xx)
   */
  get isServerError(): boolean {
    return this.statusCode !== undefined && this.statusCode >= 500 && this.statusCode < 600;
  }

  /**
   * Check if this is a rate limit error (429)
   */
  get isRateLimitError(): boolean {
    return this.statusCode === 429;
  }
}

/**
 * Error thrown when a request times out
 */
export class AIPRTimeoutError extends AIPRError {
  constructor(message = "Request timed out") {
    super(message);
    this.name = "AIPRTimeoutError";
    Object.setPrototypeOf(this, AIPRTimeoutError.prototype);
  }
}

/**
 * Error thrown when a connection error occurs
 */
export class AIPRConnectionError extends AIPRError {
  constructor(message = "Connection error") {
    super(message);
    this.name = "AIPRConnectionError";
    Object.setPrototypeOf(this, AIPRConnectionError.prototype);
  }
}
