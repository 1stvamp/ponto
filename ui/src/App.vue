<template>
  <div id="app" :class="{ cb: cbSafe }">
    <main-nav
      :counts="model.counts"
      :cbSafe="cbSafe"
      :cbColor="actionColor"
      :glyph="glyph"
      @toggleCb="cbSafe = !cbSafe"
      @saveGraph="saveGraph"
      @savePng="savePng"
      @copySummary="copySummary"
    />

    <div class="metabar">
      <span><span class="ml">dir</span> {{ dirName }}</span>
      <span class="dot">·</span>
      <span><span class="ml">plan</span> {{ planName }}</span>
      <span class="dot">·</span>
      <span><span class="ml">terraform</span> {{ tfVersion }}</span>
      <span class="dot">·</span>
      <span>{{ resourceTotal }} resources</span>
      <span class="dot">·</span>
      <span>{{ changingTotal }} changing</span>
    </div>

    <div class="main">
      <explorer
        class="sidebar"
        :model="model"
        :selectedId="selectedId"
        :search="search"
        :actionFilter="actionFilter"
        :changedOnly="changedOnly"
        :cbSafe="cbSafe"
        @update:search="search = $event"
        @update:actionFilter="actionFilter = $event"
        @update:changedOnly="changedOnly = $event"
        @select="selectResource"
      />

      <div class="graphwrap">
        <div class="graph-header">
          <span class="gh-label">DEPENDENCY GRAPH</span>
          <span class="gh-hint">{{ changedOnly ? "changed + connected" : "all resources" }}</span>
          <span class="gh-legend">
            arrow → dependency · <span class="solid">solid</span> chain ·
            <span class="dashed">dashed</span> blast radius
          </span>
          <button v-if="isNarrow" class="ov-btn" @click="openOverview">Overview</button>
        </div>
        <graph
          ref="filegraph"
          :graph="rawGraph"
          :model="model"
          :selectedId="selectedId"
          :changedOnly="changedOnly"
          :actionFilter="actionFilter"
          :cbSafe="cbSafe"
          @select="selectResource"
        />
      </div>

      <resource-detail
        class="rail"
        :class="{ 'rail-narrow': isNarrow, open: detailOpen }"
        :model="model"
        :selectedId="selectedId"
        :cbSafe="cbSafe"
        :narrow="isNarrow"
        @select="selectResource"
        @close="closeDetail"
      />
    </div>
  </div>
</template>

<script>
import axios from "axios";
import MainNav from "@/components/MainNav.vue";
import ResourceDetail from "@/components/ResourceDetail.vue";
import Graph from "@/components/Graph/Graph.vue";
import Explorer from "@/components/Explorer.vue";
import { build, actionColor, actionGlyph } from "@/model.js";

// Standalone mode injects the payloads as top-level `const` globals (map.js /
// rso.js / graph.js). Those are lexical bindings, not window properties, so we
// read them by bare name; otherwise fall back to the API.
function standaloneGlobals() {
  const g = {};
  /* eslint-disable no-undef */
  try {
    if (typeof graph !== "undefined") g.graph = graph;
  } catch (e) { /* not standalone */ }
  try {
    if (typeof map !== "undefined") g.map = map;
  } catch (e) { /* not standalone */ }
  try {
    if (typeof rso !== "undefined") g.rso = rso;
  } catch (e) { /* not standalone */ }
  /* eslint-enable no-undef */
  return g;
}

function loadPayload(globalName, apiPath, globals) {
  if (globals[globalName] !== undefined) {
    return Promise.resolve(globals[globalName]);
  }
  return axios.get(apiPath).then((r) => r.data);
}

export default {
  name: "App",
  metaInfo: { title: "Ponto | Terraform Visualization" },
  components: { MainNav, Graph, Explorer, ResourceDetail },
  data() {
    return {
      rawGraph: { nodes: [], edges: [] },
      rawMap: {},
      rawRso: {},
      model: emptyModel(),
      tfVersion: "",
      selectedId: "",
      search: "",
      actionFilter: "all",
      changedOnly: true,
      cbSafe: false,
      vw: typeof window !== "undefined" ? window.innerWidth : 1400,
      detailOpen: false,
    };
  },
  computed: {
    isNarrow() {
      return this.vw < 1080;
    },
    dirName() {
      const p = this.model.mapPath || "";
      const parts = p.split("/").filter(Boolean);
      return parts.slice(-2).join("/") || p || "—";
    },
    planName() {
      return this.rawRso && this.rawRso.__plan ? this.rawRso.__plan : "terraform plan";
    },
    resourceTotal() {
      return Object.keys(this.model.action).length;
    },
    changingTotal() {
      return (
        this.model.counts.create +
        this.model.counts.update +
        this.model.counts.delete +
        this.model.counts.replace
      );
    },
  },
  methods: {
    actionColor(a) {
      return actionColor(a, this.cbSafe);
    },
    glyph(a) {
      return actionGlyph(a);
    },
    selectResource(id) {
      this.selectedId = id;
      if (this.isNarrow) this.detailOpen = true;
    },
    openOverview() {
      this.selectedId = "";
      this.detailOpen = true;
    },
    closeDetail() {
      this.detailOpen = false;
      this.selectedId = "";
    },
    saveGraph() {
      this.$refs.filegraph.saveGraph();
    },
    savePng() {
      this.$refs.filegraph.savePng();
    },
    copySummary() {
      const c = this.model.counts;
      const text = `${c.create} to add · ${c.update} to change · ${c.delete} to destroy · ${c.replace} to replace`;
      try {
        navigator.clipboard && navigator.clipboard.writeText(text);
      } catch (e) {
        /* clipboard unavailable */
      }
    },
    onResize() {
      this.vw = window.innerWidth;
    },
  },
  mounted() {
    window.addEventListener("resize", this.onResize);
    const globals = standaloneGlobals();
    Promise.all([
      loadPayload("graph", "/api/graph", globals),
      loadPayload("map", "/api/map", globals),
      loadPayload("rso", "/api/rso", globals),
    ]).then(([graph, mapData, rso]) => {
      this.rawGraph = graph;
      this.rawMap = mapData;
      this.rawRso = rso;
      this.model = build(graph, mapData, rso);
    });
  },
  beforeDestroy() {
    window.removeEventListener("resize", this.onResize);
  },
};

function emptyModel() {
  return {
    byId: {},
    action: {},
    kind: {},
    deps: {},
    dependents: {},
    changedKeep: {},
    counts: { create: 0, update: 0, delete: 0, replace: 0, "no-op": 0 },
    managed: 0,
    fileTree: {},
    mapPath: "",
    config: {},
    states: {},
  };
}
</script>

<style>
:root {
  --bg-page: #15171a;
  --bg-panel: #121317;
  --bg-elev: #1a1b1f;
  --bg-sel: #212327;
  --border: #272a2e;
  --border-dim: #212327;
  --text: #e8e9ec;
  --text-def: #b5b8c0;
  --text-dim: #878c99;
  --text-faint: #5f6570;
  --accent: #a8ff53;
  --lav: #826dff;
  --mono: "Geist Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
  --sans: "Geist", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
}

html,
body {
  margin: 0;
  padding: 0;
  height: 100%;
  background: var(--bg-page);
  color: var(--text-def);
  font-family: var(--sans);
  -webkit-font-smoothing: antialiased;
}

#app {
  display: flex;
  flex-direction: column;
  height: 100vh;
  font-size: 13px;
}

.metabar {
  height: 30px;
  flex: 0 0 30px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 14px;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border);
  font-family: var(--mono);
  font-size: 11px;
  color: var(--text-faint);
  overflow: hidden;
  white-space: nowrap;
}
.metabar .ml {
  color: var(--text-dim);
}
.metabar .dot {
  color: var(--border);
}

.main {
  flex: 1 1 auto;
  display: flex;
  position: relative;
  min-height: 0;
}

.sidebar {
  flex: 0 0 318px;
  width: 318px;
  background: var(--bg-panel);
  border-right: 1px solid var(--border);
  overflow-y: auto;
}

.graphwrap {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: var(--bg-page);
}

.graph-header {
  height: 38px;
  flex: 0 0 38px;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 14px;
  border-bottom: 1px solid var(--border);
}
.graph-header .gh-label {
  font-size: 10.4px;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--text-dim);
}
.graph-header .gh-hint {
  font-family: var(--mono);
  font-size: 11px;
  color: var(--text-faint);
}
.graph-header .gh-legend {
  margin-left: auto;
  font-family: var(--mono);
  font-size: 10.5px;
  color: var(--text-faint);
}
.graph-header .gh-legend .solid,
.graph-header .gh-legend .dashed {
  color: var(--text-def);
}
.graph-header .ov-btn {
  margin-left: 8px;
  height: 24px;
  padding: 0 10px;
  background: var(--bg-elev);
  color: var(--text-def);
  border: 1px solid var(--border);
  border-radius: 3px;
  cursor: pointer;
  font-size: 12px;
}

.rail {
  flex: 0 0 356px;
  width: 356px;
  background: var(--bg-panel);
  border-left: 1px solid var(--border);
  overflow-y: auto;
}

.rail-narrow {
  position: absolute;
  top: 0;
  right: 0;
  height: 100%;
  max-width: 88vw;
  z-index: 30;
  box-shadow: -16px 0 44px rgba(0, 0, 0, 0.55);
  transform: translateX(101%);
  transition: transform 0.2s ease;
}
.rail-narrow.open {
  transform: translateX(0);
}
</style>
