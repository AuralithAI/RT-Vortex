// ─── POST /api/auth/clear-token ──────────────────────────────────────────────
// Clears the httpOnly auth cookie on logout.
// ─────────────────────────────────────────────────────────────────────────────

import { NextResponse } from "next/server";

export async function POST() {
  const response = NextResponse.json({ ok: true });

  response.cookies.set("token", "", {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: 0,
  });

  return response;
}
