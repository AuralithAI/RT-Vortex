// ─── Next.js Proxy — Route Protection ───────────────────────────────────────
// Redirects unauthenticated users to /login for protected routes.
// Redirects authenticated users away from /login.
// ─────────────────────────────────────────────────────────────────────────────

import { NextResponse, type NextRequest } from "next/server";

const PUBLIC_PATHS = new Set(["/login", "/callback"]);

export function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl;

  // ── API proxy: inject Authorization header from cookie ────────────────
  // Next.js rewrites don't forward cookies to the upstream server, so we
  // read the "token" cookie and attach it as a Bearer header.
  if (pathname.startsWith("/api/v1/")) {
    const token = request.cookies.get("token")?.value;
    if (token) {
      const headers = new Headers(request.headers);
      headers.set("Authorization", `Bearer ${token}`);
      return NextResponse.next({ request: { headers } });
    }
    return NextResponse.next();
  }

  // Allow public paths, static files, and local API routes
  if (
    PUBLIC_PATHS.has(pathname) ||
    pathname.startsWith("/_next") ||
    pathname.startsWith("/api/") ||
    pathname.startsWith("/favicon")
  ) {
    return NextResponse.next();
  }

  // Check for auth token cookie
  const token = request.cookies.get("token")?.value;

  if (!token) {
    const loginUrl = new URL("/login", request.url);
    loginUrl.searchParams.set("returnTo", pathname);
    return NextResponse.redirect(loginUrl);
  }

  return NextResponse.next();
}

export const config = {
  matcher: [
    /*
     * Match all request paths except:
     * - _next/static (static files)
     * - _next/image (image optimization)
     * - favicon.ico, icons, images
     */
    "/((?!_next/static|_next/image|favicon.ico|.*\\.(?:svg|png|jpg|jpeg|gif|webp)$).*)",
  ],
};
