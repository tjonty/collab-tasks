// NEXT_PUBLIC_* vars are inlined at build time, so changing the API URL
// means rebuilding the app (or the image).
export const API_URL: string =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

/** The WebSocket endpoint for a user: ws(s)://<api>/ws?userId=… */
export function wsUrl(userId: string): string {
  return `${API_URL.replace(/^http/, "ws")}/ws?userId=${encodeURIComponent(userId)}`;
}
