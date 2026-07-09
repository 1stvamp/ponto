<template>
  <div class="detail">
    <button v-if="narrow" class="rail-close" @click="$emit('close')">✕</button>

    <!-- Selected resource -->
    <div v-if="sel" class="dt">
      <div class="dt-head">
        <span class="pill" :style="pillStyle">{{ glyph }} {{ actionLabel }}</span>
        <span class="tag" :style="{ color: kindColor }">{{ kindTag }}</span>
        <span v-if="line" class="line">line #{{ line }}</span>
      </div>

      <div class="dt-id">
        <span class="idtext">{{ sel }}</span>
        <button class="copy" @click="copy">{{ copied ? "Copied" : "Copy" }}</button>
      </div>
      <div v-if="typeLine" class="typeline">{{ typeLine }}</div>

      <div class="rel">
        <div class="rel-label">DEPENDS ON</div>
        <div v-if="depends.length === 0" class="rel-empty">nothing</div>
        <div
          v-for="d in depends"
          :key="'dep' + d.id"
          class="rel-row"
          @click="$emit('select', d.id)"
        >
          <span class="rg" :style="{ color: d.color }">{{ d.glyph }}</span>{{ d.id }}
        </div>
      </div>

      <div class="rel">
        <div class="rel-label">BLAST RADIUS <span class="rl-sub">— depended on by</span></div>
        <div v-if="dependents.length === 0" class="rel-empty">nothing</div>
        <div
          v-for="d in dependents"
          :key="'dpt' + d.id"
          class="rel-row"
          @click="$emit('select', d.id)"
        >
          <span class="rg" :style="{ color: d.color }">{{ d.glyph }}</span>{{ d.id }}
        </div>
      </div>

      <div class="tabs">
        <button :class="{ active: tab === 'config' }" @click="tab = 'config'">Config</button>
        <button :class="{ active: tab === 'current' }" @click="tab = 'current'">Current State</button>
        <button :class="{ active: tab === 'proposed' }" @click="tab = 'proposed'">Proposed State</button>
      </div>
      <div class="tab-body">
        <div v-if="tabRows.length === 0" class="tab-empty">{{ tabEmpty }}</div>
        <div v-for="kv in tabRows" :key="kv.key" class="kv">
          <div class="k">{{ kv.key }}</div>
          <div class="v" :class="{ dim: kv.dim }">{{ kv.value }}</div>
        </div>
      </div>
    </div>

    <!-- Overview when nothing is selected -->
    <div v-else class="ov">
      <div class="ov-title">PLAN OVERVIEW</div>
      <div
        v-for="o in overviewRows"
        :key="o.label"
        class="ov-row"
        :style="{ borderLeftColor: o.count === 0 ? '#2C3034' : o.color }"
      >
        <span class="ov-count">{{ o.count }}</span>
        <span class="ov-glyph" :style="{ color: o.count === 0 ? '#4D525B' : o.color }">{{ o.glyph }}</span>
        <span class="ov-label">{{ o.label }}</span>
      </div>

      <div class="ov-title">LEGEND</div>
      <div class="legend">
        <div v-for="k in legend" :key="k.tag" class="lg">
          <span class="lg-tag" :style="{ color: k.color }">{{ k.tag }}</span>
          <span class="lg-label">{{ k.label }}</span>
        </div>
      </div>

      <div class="hint">Click a node in the graph or a row in the list to inspect it.</div>
    </div>
  </div>
</template>

<script>
import { ACT, KIND, actionColor, actionGlyph } from "@/model.js";

export default {
  name: "ResourceDetail",
  props: {
    model: { type: Object, required: true },
    selectedId: String,
    cbSafe: Boolean,
    narrow: Boolean,
  },
  data() {
    return { tab: "config", copied: false };
  },
  computed: {
    sel() {
      return this.selectedId;
    },
    action() {
      return this.model.action[this.sel] || "no-op";
    },
    glyph() {
      return actionGlyph(this.action);
    },
    actionLabel() {
      return this.action || "no-op";
    },
    aColor() {
      return actionColor(this.action, this.cbSafe);
    },
    pillStyle() {
      return { color: this.aColor, background: this.aColor + "1F", borderColor: this.aColor + "55" };
    },
    kindDef() {
      return KIND[this.model.kind[this.sel]] || KIND.resource;
    },
    kindTag() {
      return this.kindDef.tag;
    },
    kindColor() {
      return this.kindDef.c;
    },
    line() {
      return this.model.lines[this.sel] || 0;
    },
    typeLine() {
      const rt = this.model.rtype[this.sel];
      if (!rt) return "";
      const kind = this.model.kind[this.sel];
      return rt + (kind === "data" ? " · data source" : " · managed resource");
    },
    depends() {
      return this.relRows(this.model.deps[this.sel] || []);
    },
    dependents() {
      return this.relRows(this.model.dependents[this.sel] || []);
    },
    overviewRows() {
      const c = this.model.counts;
      const defs = [
        ["create", "add"],
        ["update", "change"],
        ["delete", "destroy"],
        ["replace", "replace"],
      ];
      return defs.map(([a, label]) => ({
        label,
        count: c[a] || 0,
        glyph: ACT[a].g,
        color: actionColor(a, this.cbSafe),
      }));
    },
    legend() {
      return [
        { tag: "RES", label: "Resource", color: KIND.resource.c },
        { tag: "DATA", label: "Data source", color: KIND.data.c },
        { tag: "VAR", label: "Variable", color: KIND.variable.c },
        { tag: "OUT", label: "Output", color: KIND.output.c },
        { tag: "MOD", label: "Module", color: KIND.module.c },
        { tag: "LOCAL", label: "Local", color: KIND.local.c },
      ];
    },
    tabEmpty() {
      if (this.tab === "config") return "No configurable attributes.";
      if (this.tab === "current") {
        return this.action === "create" ? "Resource doesn't currently exist." : "No current state.";
      }
      return this.action === "delete" ? "Resource will be destroyed." : "No proposed state.";
    },
    tabRows() {
      if (this.tab === "config") {
        return (this.model.config[this.sel] || []).map((kv) => ({ key: kv.key, value: kv.value, dim: false }));
      }
      const st = this.model.states[this.sel];
      const vals = st && st.values ? st.values : {};
      return Object.keys(vals)
        .sort()
        .map((k) => {
          const v = vals[k];
          const empty = v === null || v === undefined;
          return { key: k, value: empty ? "(known after apply)" : format(v), dim: empty };
        });
    },
  },
  watch: {
    selectedId() {
      this.copied = false;
      this.tab = "config";
    },
  },
  methods: {
    relRows(ids) {
      const seen = {};
      const out = [];
      for (const id of ids) {
        if (seen[id]) continue;
        seen[id] = true;
        const a = this.model.action[id] || "no-op";
        out.push({ id, glyph: actionGlyph(a), color: actionColor(a, this.cbSafe) });
      }
      return out.sort((x, y) => x.id.localeCompare(y.id));
    },
    copy() {
      try {
        navigator.clipboard && navigator.clipboard.writeText(this.sel);
      } catch (e) {
        /* clipboard unavailable */
      }
      this.copied = true;
      setTimeout(() => (this.copied = false), 1200);
    },
  },
};

function format(v) {
  if (typeof v === "string") return v;
  return JSON.stringify(v);
}
</script>

<style scoped>
.detail {
  padding: 14px;
  position: relative;
}
.rail-close {
  position: absolute;
  top: 10px;
  right: 10px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  color: var(--text-def);
  border-radius: 3px;
  width: 24px;
  height: 24px;
  cursor: pointer;
}

.dt-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}
.pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  padding: 2px 8px;
  border: 1px solid;
  border-radius: 3px;
  font-family: var(--mono);
}
.tag {
  font-family: var(--mono);
  font-size: 10px;
}
.line {
  margin-left: auto;
  color: var(--text-faint);
  font-family: var(--mono);
  font-size: 11px;
}

.dt-id {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 4px;
}
.idtext {
  font-family: var(--mono);
  font-size: 13px;
  color: var(--text);
  word-break: break-all;
  flex: 1;
}
.copy {
  background: var(--bg-elev);
  border: 1px solid var(--border);
  color: var(--text-def);
  border-radius: 3px;
  font-size: 11px;
  padding: 3px 8px;
  cursor: pointer;
}
.typeline {
  color: var(--text-faint);
  font-family: var(--mono);
  font-size: 11px;
  margin-bottom: 14px;
}

.rel {
  margin-bottom: 14px;
}
.rel-label {
  font-size: 10.4px;
  letter-spacing: 0.04em;
  color: var(--text-dim);
  margin-bottom: 6px;
}
.rel-label .rl-sub {
  color: var(--text-faint);
  text-transform: none;
  letter-spacing: 0;
}
.rel-empty {
  color: var(--text-faint);
  font-style: italic;
  font-size: 12px;
}
.rel-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-family: var(--mono);
  font-size: 12px;
  color: var(--text-def);
  padding: 2px 0;
  cursor: pointer;
}
.rel-row:hover {
  color: var(--text);
}
.rel-row .rg {
  font-weight: 600;
  width: 10px;
  text-align: center;
}

.tabs {
  display: flex;
  gap: 14px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 10px;
}
.tabs button {
  background: transparent;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--text-dim);
  padding: 6px 0;
  cursor: pointer;
  font-size: 12px;
}
.tabs button.active {
  color: var(--text);
  border-bottom-color: var(--accent);
}
.tab-empty {
  color: var(--text-faint);
  font-size: 12px;
  font-style: italic;
}
.kv {
  margin-bottom: 8px;
}
.kv .k {
  font-size: 10.4px;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--text-dim);
  margin-bottom: 3px;
}
.kv .v {
  background: var(--bg-elev);
  border: 1px solid var(--bg-sel);
  border-radius: 4px;
  padding: 6px 8px;
  font-family: var(--mono);
  font-size: 12px;
  color: var(--text-def);
  word-break: break-all;
}
.kv .v.dim {
  color: var(--text-faint);
  font-style: italic;
}

/* overview */
.ov-title {
  font-size: 10.4px;
  letter-spacing: 0.04em;
  color: var(--text-dim);
  margin: 6px 0 8px;
}
.ov-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 12px;
  border-left: 2px solid;
  background: var(--bg-elev);
  border-radius: 0 4px 4px 0;
  margin-bottom: 6px;
}
.ov-count {
  font-size: 22px;
  color: var(--text);
  font-variant-numeric: tabular-nums;
  min-width: 24px;
}
.ov-glyph {
  font-family: var(--mono);
  font-weight: 600;
}
.ov-label {
  color: var(--text-def);
}
.legend {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px;
  margin-bottom: 16px;
}
.lg {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 9px;
  background: var(--bg-elev);
  border-left: 2px solid;
  border-color: transparent;
  border-radius: 3px;
}
.lg-tag {
  font-family: var(--mono);
  font-size: 10px;
}
.lg-label {
  color: var(--text-def);
  font-size: 12px;
}
.hint {
  color: var(--text-faint);
  font-size: 12px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 12px;
}
</style>
