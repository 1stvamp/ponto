// Shared plan model: derives everything the UI needs from the graph, map and
// rso payloads (served at /api/* or injected as globals in standalone mode).
// Keeping the derivation in one place keeps the components thin and consistent.

// Action -> glyph + normal/colour-blind-safe colours. Shared with the TUI legend.
export const ACT = {
  create: { g: "+", n: "#4EE88E", cb: "#2CA089" },
  update: { g: "~", n: "#60A5FA", cb: "#5AA9E6" },
  delete: { g: "−", n: "#FB5C78", cb: "#E06D2E" },
  replace: { g: "±", n: "#FBBF24", cb: "#E8A33D" },
  "no-op": { g: "·", n: "#5F6570", cb: "#6E7480" },
  read: { g: "·", n: "#5F6570", cb: "#6E7480" },
  "": { g: "·", n: "#5F6570", cb: "#6E7480" },
};

// Kind -> tag + accent colour. Lets kind read without relying on colour alone.
export const KIND = {
  resource: { tag: "RES", c: "#878C99" },
  data: { tag: "DATA", c: "#FF7AD9" },
  variable: { tag: "VAR", c: "#68BAF2" },
  output: { tag: "OUT", c: "#F5D547" },
  module: { tag: "MOD", c: "#A78BFA" },
  local: { tag: "LOCAL", c: "#878C99" },
};

export const CHANGED_ACTIONS = ["create", "update", "delete", "replace"];

export function actionColor(action, cbSafe) {
  const a = ACT[action || ""] || ACT[""];
  return cbSafe ? a.cb : a.n;
}

export function actionGlyph(action) {
  return (ACT[action || ""] || ACT[""]).g;
}

// kindFromClasses maps a graph node's leading class to a UI kind.
function kindFromClasses(classes, type) {
  const c = (classes || "").split(" ")[0] || "";
  if (c.startsWith("resource")) return "resource";
  if (c.startsWith("data")) return "data";
  if (c === "output") return "output";
  if (c === "variable") return "variable";
  if (c === "module") return "module";
  if (c === "locals" || c === "local") return "local";
  // fall back to the node type
  if (type === "locals") return "local";
  if (KIND[type]) return type;
  return "resource";
}

// isLeafRow marks graph nodes that are real resource/data instances (list rows),
// not the "-type" container nodes or files.
function isLeafRow(node) {
  return /(^|\s)(resource-name|data-name)(\s|$)/.test(node.classes || "");
}

// build derives the shared model from the three payloads.
export function build(graph, mapData, rso) {
  const nodes = (graph && graph.nodes) || [];
  const edges = (graph && graph.edges) || [];

  const byId = {};
  const action = {};
  const kind = {};
  for (const n of nodes) {
    const d = n.data || {};
    kind[d.id] = kindFromClasses(n.classes, d.type);
    if (isLeafRow(n)) {
      action[d.id] = d.change || "no-op";
    } else if (d.change) {
      action[d.id] = d.change;
    }
    byId[d.id] = {
      id: d.id,
      label: d.label || d.id,
      kind: kind[d.id],
      action: action[d.id] || "",
      type: d.type,
    };
  }

  // edges: source depends on target
  const deps = {};
  const dependents = {};
  for (const e of edges) {
    const s = e.data.source;
    const t = e.data.target;
    (deps[s] = deps[s] || []).push(t);
    (dependents[t] = dependents[t] || []).push(s);
  }

  // changed keep set: seed (managed leaves that change) + transitive downstream
  // (dependents) + 1-hop upstream (direct dependencies). Matches the TUI.
  const seed = [];
  for (const n of nodes) {
    if (isLeafRow(n) && CHANGED_ACTIONS.includes(n.data.change)) {
      seed.push(n.data.id);
    }
  }
  const changedKeep = {};
  seed.forEach((id) => (changedKeep[id] = true));
  const queue = seed.slice();
  while (queue.length) {
    const id = queue.pop();
    for (const s of dependents[id] || []) {
      if (!changedKeep[s]) {
        changedKeep[s] = true;
        queue.push(s);
      }
    }
  }
  seed.forEach((id) => {
    for (const t of deps[id] || []) changedKeep[t] = true;
  });

  // counts over managed resource leaves (terraform convention)
  const counts = { create: 0, update: 0, delete: 0, replace: 0, "no-op": 0 };
  let managed = 0;
  for (const n of nodes) {
    if (isLeafRow(n) && kind[n.data.id] === "resource") {
      managed++;
      const a = n.data.change || "no-op";
      if (counts[a] !== undefined) counts[a]++;
    }
  }

  // line numbers and resource types come from the map file/module tree
  const lines = {};
  const rtype = {};
  walkMap((mapData && mapData.root) || {}, (id, node) => {
    if (node.line) lines[id] = node.line;
    if (node.resource_type) rtype[id] = node.resource_type;
  });

  return {
    byId,
    action,
    kind,
    deps,
    dependents,
    changedKeep,
    counts,
    managed,
    lines,
    rtype,
    fileTree: (mapData && mapData.root) || {},
    mapPath: (mapData && mapData.path) || "",
    config: buildConfig(rso),
    states: buildStates(rso),
  };
}

// walkMap visits every node in the map file/module tree.
function walkMap(root, visit) {
  const rec = (obj) => {
    for (const [id, node] of Object.entries(obj || {})) {
      if (!node || typeof node !== "object") continue;
      visit(id, node);
      if (node.children) rec(node.children);
    }
  };
  rec(root);
}

// buildConfig flattens rso.configs into id -> [{key, value}] pairs.
function buildConfig(rso) {
  const out = {};
  const configs = (rso && rso.configs) || {};
  for (const id of Object.keys(configs)) {
    const c = configs[id] || {};
    const rc = c.resource_config || c.ResourceConfig;
    const expr = rc && rc.expressions;
    if (!expr) continue;
    const pairs = [];
    for (const k of Object.keys(expr).sort()) {
      const v = exprValue(expr[k]);
      if (v !== "") pairs.push({ key: k, value: v });
    }
    if (pairs.length) out[id] = pairs;
  }
  return out;
}

function exprValue(e) {
  if (!e) return "";
  if (Array.isArray(e.references) && e.references.length) return e.references[0];
  if (Object.prototype.hasOwnProperty.call(e, "constant_value")) {
    const cv = e.constant_value;
    if (cv === null || cv === undefined) return "";
    if (typeof cv === "string") return JSON.stringify(cv);
    return String(cv);
  }
  return "";
}

// buildStates flattens rso.states into id -> {before, after} attribute maps.
function buildStates(rso) {
  const out = {};
  const states = (rso && rso.states) || {};
  const walk = (mod, prefix) => {
    if (!mod || !mod.module) return;
    for (const res of mod.module.resources || []) {
      const id = prefix + res.address;
      out[id] = { values: res.values || {} };
    }
  };
  for (const key of Object.keys(states)) {
    const prefix = key ? "module." + key + "." : "";
    walk(states[key], prefix);
  }
  return out;
}
