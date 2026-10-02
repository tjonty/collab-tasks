"use client";

import { useEffect, useState } from "react";

import { API_URL } from "@/lib/config";

type Health = Record<string, "ok" | "down">;

async function checkApi(): Promise<string> {
  try {
    const res = await fetch(`${API_URL}/healthz`, { cache: "no-store" });
    if (res.status === 200) {
      return "API: ok";
    }
    if (res.status === 503) {
      const health: Health = await res.json();
      const down = Object.keys(health).filter((k) => health[k] === "down");
      return `API: degraded (${down.join(", ")} down)`;
    }
    return "API: unreachable";
  } catch {
    return "API: unreachable";
  }
}

export function ApiStatus() {
  const [status, setStatus] = useState("Checking API…");

  useEffect(() => {
    let ignore = false;
    checkApi().then((text) => {
      if (!ignore) setStatus(text);
    });
    return () => {
      ignore = true;
    };
  }, []);

  return <p role="status">{status}</p>;
}
