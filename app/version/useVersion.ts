import { useEffect, useState } from "react";

import type { Info } from "@/types/generated/version";

// useVersion asks the server which build it's running. It's undefined until
// the server answers, and stays undefined if it doesn't, like when the
// frontend runs without an API.
export function useVersion(): string | undefined {
  const [version, setVersion] = useState<string>();

  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/version", { signal: controller.signal })
      .then((response) => (response.ok ? response.json() : undefined))
      .then((info: Info | undefined) => setVersion(info?.version))
      .catch(() => {});
    return () => controller.abort();
  }, []);

  return version;
}
