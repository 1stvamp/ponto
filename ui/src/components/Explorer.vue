<template>
  <div class="explorer">
    <div class="ex-head">
      <span class="ex-label">RESOURCES</span>
      <div class="seg">
        <button :class="{ active: changedOnly }" @click="$emit('update:changedOnly', true)">Changed</button>
        <button :class="{ active: !changedOnly }" @click="$emit('update:changedOnly', false)">All</button>
      </div>
    </div>

    <div class="ex-search">
      <span class="mag">⌕</span>
      <input
        :value="search"
        placeholder="Filter resources…"
        @input="$emit('update:search', $event.target.value)"
      />
      <span v-if="search" class="clear" @click="$emit('update:search', '')">✕</span>
    </div>

    <div class="ex-chips">
      <button
        v-for="c in chips"
        :key="c.key"
        class="chip"
        :class="{ active: actionFilter === c.key }"
        :style="chipStyle(c)"
        @click="toggleChip(c.key)"
      >
        <span v-if="c.glyph" class="cg">{{ c.glyph }}</span>{{ c.label }} {{ c.count }}
      </button>
    </div>

    <div class="ex-list">
      <template v-for="row in rows">
        <div
          v-if="row.type === 'file'"
          :key="row.key"
          class="file-hd"
          @click="toggleFile(row.id)"
        >
          <span class="caret">{{ isFileOpen(row.id) ? "▾" : "▸" }}</span>
          <span class="fname">{{ row.name }}</span>
          <span class="fcount">{{ row.count }}</span>
        </div>
        <div
          v-else
          :key="row.key"
          class="res-row"
          :class="{ selected: row.id === selectedId }"
          :style="{ paddingLeft: 10 + row.depth * 14 + 'px', borderLeftColor: row.accent }"
          @click="$emit('select', row.id)"
        >
          <span
            v-if="row.hasChildren"
            class="rcaret"
            @click.stop="toggleExpand(row.id)"
          >{{ isExpanded(row.id) ? "▾" : "▸" }}</span>
          <span v-else class="rcaret-sp"></span>
          <span class="rglyph" :style="{ color: row.actionColor }">{{ row.glyph }}</span>
          <span class="rname">{{ row.name }}</span>
          <span v-if="row.subtype" class="rtype">{{ row.subtype }}</span>
          <span class="rtag" :style="{ color: row.kindColor }">{{ row.tag }}</span>
          <span v-if="row.line" class="rline">#{{ row.line }}</span>
        </div>
      </template>
      <div v-if="rows.length === 0" class="empty">No resources match. Reset filters.</div>
    </div>
  </div>
</template>

<script>
import { ACT, KIND, actionColor, actionGlyph } from "@/model.js";

export default {
  name: "Explorer",
  props: {
    model: { type: Object, required: true },
    selectedId: String,
    search: String,
    actionFilter: String,
    changedOnly: Boolean,
    cbSafe: Boolean,
  },
  data() {
    return { openFiles: {}, expanded: {} };
  },
  computed: {
    chips() {
      const c = this.model.counts;
      const total =
        (c.create || 0) + (c.update || 0) + (c.delete || 0) + (c.replace || 0) + (c["no-op"] || 0);
      return [
        { key: "all", label: "All", glyph: "", count: total },
        { key: "create", label: "create", glyph: ACT.create.g, count: c.create || 0 },
        { key: "update", label: "update", glyph: ACT.update.g, count: c.update || 0 },
        { key: "delete", label: "delete", glyph: ACT.delete.g, count: c.delete || 0 },
        { key: "replace", label: "replace", glyph: ACT.replace.g, count: c.replace || 0 },
        { key: "no-op", label: "no-op", glyph: ACT["no-op"].g, count: c["no-op"] || 0 },
      ];
    },
    rows() {
      const out = [];
      const root = this.model.fileTree || {};
      for (const [id, node] of Object.entries(root)) {
        this.walk(id, node, 0, out);
      }
      return out;
    },
  },
  methods: {
    chipStyle(c) {
      if (this.actionFilter !== c.key) return {};
      const color = c.key === "all" ? "var(--accent)" : actionColor(c.key, this.cbSafe);
      return { background: color, color: "#0B0C0F", borderColor: color };
    },
    toggleChip(key) {
      this.$emit("update:actionFilter", this.actionFilter === key ? "all" : key);
    },
    toggleFile(id) {
      this.$set(this.openFiles, id, !this.isFileOpen(id));
    },
    isFileOpen(id) {
      return this.openFiles[id] !== false;
    },
    toggleExpand(id) {
      this.$set(this.expanded, id, !this.isExpanded(id));
    },
    isExpanded(id) {
      return this.expanded[id] !== false;
    },
    // matches decides if a leaf id passes the active filters.
    matches(id) {
      const m = this.model;
      if (this.changedOnly && !m.changedKeep[id]) return false;
      if (this.actionFilter !== "all") {
        const a = m.action[id] || "no-op";
        if (a !== this.actionFilter) return false;
      }
      if (this.search) {
        const q = this.search.toLowerCase();
        if (!id.toLowerCase().includes(q)) return false;
      }
      return true;
    },
    // subtreeMatches returns true if this node or any descendant passes filters.
    subtreeMatches(id, node) {
      if (node.type === "file" || node.type === "module") {
        return Object.entries(node.children || {}).some(([cid, c]) => this.subtreeMatches(cid, c));
      }
      if (node.children && Object.keys(node.children).length) {
        if (this.matches(id)) return true;
        return Object.entries(node.children).some(([cid, c]) => this.subtreeMatches(cid, c));
      }
      return this.matches(id);
    },
    // walk flattens the map tree into display rows honouring filters + collapse.
    walk(id, node, depth, out) {
      const m = this.model;
      if (node.type === "file") {
        if (!this.subtreeMatches(id, node)) return;
        const start = out.length;
        const header = { type: "file", key: "f:" + id + depth, id, name: node.name, depth, count: 0 };
        out.push(header);
        if (this.isFileOpen(id)) {
          for (const [cid, c] of Object.entries(node.children || {})) {
            this.walk(cid, c, depth, out);
          }
        }
        header.count = out.length - start - 1;
        if (header.count === 0 && !this.isFileOpen(id)) header.count = this.countLeaves(node);
        else header.count = this.countLeaves(node);
        return;
      }

      // resource / data / output / variable / local / module row
      const hasChildren = !!(node.children && Object.keys(node.children).length);
      const leaf = !hasChildren;
      if (leaf && !this.matches(id)) return;
      if (hasChildren && !this.subtreeMatches(id, node)) return;

      const kind = m.kind[id] || mapKind(node.type);
      const action = m.action[id] || node.change_action || "";
      const kindDef = KIND[kind] || KIND.resource;
      const accent =
        kind === "resource" || kind === "data" ? actionColor(action, this.cbSafe) : kindDef.c;

      out.push({
        type: "res",
        key: "r:" + id + depth,
        id,
        name: node.name || id,
        subtype: node.resource_type || "",
        depth,
        line: node.line || 0,
        glyph: actionGlyph(action),
        actionColor: actionColor(action, this.cbSafe),
        kindColor: kindDef.c,
        tag: kindDef.tag,
        hasChildren,
        accent,
      });

      if (hasChildren && this.isExpanded(id)) {
        // A module contains files; parent resources contain instances.
        for (const [cid, c] of Object.entries(node.children)) {
          if (c.type === "file") {
            for (const [gid, g] of Object.entries(c.children || {})) {
              this.walk(gid, g, depth + 1, out);
            }
          } else {
            this.walk(cid, c, depth + 1, out);
          }
        }
      }
    },
    countLeaves(node) {
      let n = 0;
      const rec = (nd) => {
        for (const c of Object.values(nd.children || {})) {
          if (c.children && (c.type === "file" || c.type === "module")) rec(c);
          else if (c.children) {
            n++;
            rec(c);
          } else n++;
        }
      };
      rec(node);
      return n;
    },
  },
};

function mapKind(type) {
  if (type === "locals") return "local";
  return KIND[type] ? type : "resource";
}
</script>

<style scoped>
.explorer {
  padding: 12px 12px 24px;
}
.ex-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}
.ex-label {
  font-size: 10.4px;
  letter-spacing: 0.04em;
  color: var(--text-dim);
}
.seg {
  display: inline-flex;
  background: var(--bg-elev);
  border-radius: 3px;
  padding: 2px;
}
.seg button {
  border: 0;
  background: transparent;
  color: var(--text-dim);
  font-size: 11px;
  padding: 3px 9px;
  border-radius: 2px;
  cursor: pointer;
}
.seg button.active {
  background: #2c3034;
  color: var(--text);
}

.ex-search {
  position: relative;
  display: flex;
  align-items: center;
  height: 32px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 3px;
  padding: 0 8px;
  margin-bottom: 10px;
}
.ex-search .mag {
  color: var(--text-faint);
  font-size: 13px;
}
.ex-search input {
  flex: 1;
  background: transparent;
  border: 0;
  outline: none;
  color: var(--text);
  font-family: var(--mono);
  font-size: 12px;
  padding: 0 6px;
}
.ex-search .clear {
  cursor: pointer;
  color: var(--text-faint);
}

.ex-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-bottom: 12px;
}
.ex-chips .chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  padding: 3px 7px;
  border: 1px solid var(--border);
  border-radius: 3px;
  background: var(--bg-elev);
  color: var(--text-def);
  cursor: pointer;
}
.ex-chips .chip .cg {
  font-family: var(--mono);
  font-weight: 600;
}

.file-hd {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 2px 4px;
  cursor: pointer;
}
.file-hd .caret {
  color: var(--text-faint);
  font-size: 10px;
  width: 10px;
}
.file-hd .fname {
  font-family: var(--mono);
  font-size: 11px;
  color: var(--text-def);
}
.file-hd .fcount {
  color: var(--text-faint);
  font-size: 11px;
}

.res-row {
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 28px;
  padding: 4px 8px 4px 10px;
  border-left: 2px solid transparent;
  border-radius: 0 4px 4px 0;
  cursor: pointer;
}
.res-row:hover {
  background: var(--bg-elev);
}
.res-row.selected {
  background: var(--bg-sel);
}
.rcaret {
  width: 10px;
  font-size: 10px;
  color: var(--text-faint);
  cursor: pointer;
}
.rcaret-sp {
  width: 10px;
}
.rglyph {
  font-family: var(--mono);
  font-weight: 600;
  width: 10px;
  text-align: center;
}
.rname {
  color: #d7d9dd;
  font-family: var(--mono);
  font-size: 12px;
}
.rtype {
  color: var(--text-faint);
  font-family: var(--mono);
  font-size: 11px;
}
.rtag {
  margin-left: auto;
  font-family: var(--mono);
  font-size: 9.5px;
  letter-spacing: 0.03em;
}
.rline {
  color: var(--text-faint);
  font-family: var(--mono);
  font-size: 11px;
  min-width: 26px;
  text-align: right;
}
.empty {
  color: var(--text-faint);
  font-size: 12px;
  padding: 16px 4px;
}
</style>
