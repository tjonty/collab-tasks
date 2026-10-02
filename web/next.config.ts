import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Emit .next/standalone (server.js + minimal node_modules) for the
  // Docker runtime image.
  output: "standalone",
};

export default nextConfig;
