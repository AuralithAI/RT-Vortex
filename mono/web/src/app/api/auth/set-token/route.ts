// ─── POST /api/auth/set-token ────────────────────────────────────────────────
// Called from /callback after OAuth redirect. Sets the JWT in an httpOnly
// cookie so it's automatically sent on every same-origin request.
// ─────────────────────────────────────────────────────────────────────────────

import { NextResponse, type NextRequest } from "next/server";

export async function POST(request: NextRequest) {
  try {
    const { token } = (await request.json()) as { token?: string };

    if (!token || typeof token !== "string") {
      return NextResponse.json({ error: "Token is required" }, { status: 400 });
    }

    const response = NextResponse.json({ ok: true });

    response.cookies.set("token", token, {
      httpOnly: false, // readable by JS so the API client can set Authorization header
      secure: process.env.NODE_ENV === "production",
      sameSite: "lax",
      path: "/",
      maxAge: 60 * 60 * 24 * 7, // 7 days
    });

    return response;
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 });
  }
}
