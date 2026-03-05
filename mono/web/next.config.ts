import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Option A — standalone for Docker / self-hosted with nginx.
  output: "standalone",
  // Option B — static export for CDN (S3+CloudFront, Vercel, Netlify):
  //   Set NEXT_OUTPUT=export and rebuild.
  ...(process.env.NEXT_OUTPUT === "export" ? { output: "export" } : {}),

  reactCompiler: true,
  reactStrictMode: true,

  images: {
    unoptimized: process.env.NEXT_OUTPUT === "export",
  },

  // Dev-mode proxy: rewrite /api/* → Go server on port 8080.
  // In production nginx handles this (Option A) or NEXT_PUBLIC_API_URL is set (Option B).
  async rewrites() {
    if (process.env.NEXT_OUTPUT === "export") return [];
    const apiUrl = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
    return [
      { source: "/api/:path*", destination: `${apiUrl}/api/:path*` },
    ];
  },
};

export default nextConfig;
