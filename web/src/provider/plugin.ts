import type { ProviderPlugin } from "@core/contract";

export const plugin: ProviderPlugin = {
  id: "clinepass",
  name: "ClinePass",
  repo: "https://github.com/berry-shake/cpa-clinepass-plugin",
  models: {
    matches: (m, filter) =>
      [m.name, m.description, m.upstream_id, ...(Array.isArray(m.tags) ? m.tags : [])]
        .some((v) => String(v || "").toLowerCase().includes(filter)),
  },
  editor: {
    init: () => ({}),
    pickName: (_t, m) => ({
      text: Array.isArray(m.tags) ? m.tags.join(" · ") : "",
      title: m.description || m.upstream_id,
    }),
    // The catalog has no context or price figures; the description says the most.
    modelMeta: (_t, m) => m.description || "",
  },
};
