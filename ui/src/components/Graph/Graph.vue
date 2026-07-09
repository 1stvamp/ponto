<template>
  <div class="graph-canvas">
    <p v-if="noChangeNote" class="graph-note">
      No resource changes in this plan, so the full graph is shown.
    </p>
    <p v-if="guardActive" class="graph-note">
      Showing all {{ graph.nodes.length }} resources. Large graphs can be slow
      to render in the browser; for big plans the static image
      (<code>-genImage</code>) is faster.
    </p>
    <cytoscape ref="cy" :config="config" :preConfig="preConfig"></cytoscape>
  </div>
</template>

<script>
import { saveAs } from "file-saver";
import klay from "cytoscape-klay";
import svg from 'cytoscape-svg';
import nodeHtmlLabel from "cytoscape-node-html-label";

const config = {
  autounselectify: true,
  style: [
    {
      selector: "node",
      style: {
        label: "data(label)",
        width: "500px",
        "font-family": "Avenir, Helvetica, Arial, sans-serif",
        "font-size": "2em",
      },
    },
    {
      selector: "edge",
      css: {
        "curve-style": "taxi",
        "line-color": "#4D525B",
        "target-arrow-color": "#5F6570",
        "target-arrow-shape": "triangle",
        "arrow-scale": 1.2,
        opacity: 0.5,
        width: 6,
      },
    },
    {
      selector: ".basename",
      style: {
        padding: "200px",
        "text-margin-y": 75,
        "font-weight": "bold",
        shape: "roundrectangle",
        "min-height": "400px",
        "border-width": 2,
        "border-color": "#272A2E",
        color: "#878C99",
        "background-color": "#121317",
      },
    },
    {
      selector: ".fname",
      style: {
        padding: "100px",
        "text-margin-y": 75,
        "font-weight": "bold",
        shape: "roundrectangle",
        "border-width": 1,
        "border-color": "#272A2E",
        color: "#878C99",
        "background-color": "#15171A",
      },
    },
    {
      selector: ".provider",
      style: {
        "text-valign": "center",
        "text-halign": "center",
        padding: "1em",
        shape: "roundrectangle",
        "border-width": 0,
        color: "white",
        "background-color": "black",
      },
    },
    {
      selector: ".module",
      style: {
        padding: "100px",
        "font-weight": "bold",
        "text-margin-y": 60,
        shape: "roundrectangle",
        color: "#A78BFA",
        "border-width": 4,
        "border-color": "#A78BFA",
        "background-color": "#121317",
      },
    },
    {
      selector: ".data-type",
      style: {
        padding: "10%",
        width: "label",
        "font-weight": "bold",
        color: "#B5B8C0",
        "text-background-color": "#15171A",
        "text-background-opacity": 1,
        "text-background-padding": "2em",
        "text-margin-y": 15,
        shape: "roundrectangle",
        "border-width": "3px",
        "border-color": "#272A2E",
        "background-color": "#15171A",
        // "text-background-color": "data(parentColor)",
        // "background-color": "data(parentColor)",
      },
    },
    {
      selector: ".data-name",
      css: {
        "font-weight": "bold",
        "text-valign": "center",
        "text-halign": "center",
        padding: "1.5em",
        shape: "roundrectangle",
        "border-opacity": 1,
        "border-width": 3,
        "border-color": "#FF7AD9",
        "background-color": "#1A1B1F",
        color: "#E8E9EC",
        label: "data(label)",
      },
    },
    {
      selector: ".output",
      css: {
        "background-color": "#1A1B1F",
        color: "#E8E9EC",
        "font-weight": "bold",
        "text-valign": "center",
        "text-halign": "center",
        padding: "1.5em",
        shape: "roundrectangle",
        "border-opacity": 1,
        "border-width": 3,
        "border-color": "#F5D547",
        label: "data(label)",
      },
    },
    {
      selector: ".variable",
      css: {
        "background-color": "#1A1B1F",
        color: "#E8E9EC",
        "font-weight": "bold",
        "text-valign": "center",
        "text-halign": "center",
        padding: "1.5em",
        shape: "roundrectangle",
        "border-opacity": 1,
        "border-width": 3,
        "border-color": "#68BAF2",
        label: "data(label)",
      },
    },
    {
      selector: ".locals",
      css: {
        "background-color": "#1A1B1F",
        color: "#E8E9EC",
        "font-weight": "bold",
        "text-valign": "center",
        "text-halign": "center",
        padding: "1.5em",
        shape: "roundrectangle",
        "border-opacity": 1,
        "border-width": 3,
        "border-color": "#878C99",
        label: "data(label)",
      },
    },
    {
      selector: ".resource-type",
      style: {
        padding: "10%",
        width: "label",
        "font-weight": "bold",
        color: "#B5B8C0",
        "text-background-color": "#15171A",
        "text-background-opacity": 1,
        "text-background-padding": "2em",
        "text-margin-y": 15,
        shape: "roundrectangle",
        "border-width": "3px",
        "border-color": "#272A2E",
        "background-color": "#15171A",
        // "text-background-color": "data(parentColor)",
        // "background-color": "data(parentColor)",
      },
    },
    {
      selector: ".resource-parent",
      style: {
        padding: "10%",
        width: "label",
        "font-weight": "bold",
        color: "#B5B8C0",
        "text-background-color": "#15171A",
        "text-background-opacity": 1,
        "text-background-padding": "2em",
        "text-margin-y": 15,
        shape: "roundrectangle",
        "border-width": "3px",
        "border-color": "#272A2E",
        "background-color": "#15171A",
        // "text-background-color": "data(parentColor)",
        // "background-color": "data(parentColor)",
      },
    },
    {
      selector: ".resource-name",
      css: {
        "text-valign": "center",
        "text-halign": "center",
        padding: "1.5em",
        shape: "roundrectangle",
        "border-opacity": 1,
        "border-width": "3px",
        "border-color": "#272A2E",
        color: "#E8E9EC",
        "background-color": "#1A1B1F",
        "text-wrap": "ellipsis",
        "text-max-width": 500,
      },
    },
    {
      selector: ".create",
      css: {
        "background-color": "#1A1B1F",
        color: "#E8E9EC",
        "border-color": "#4EE88E",
        "font-weight": "bold",
      },
    },
    {
      selector: ".delete",
      css: {
        "background-color": "#1A1B1F",
        color: "#E8E9EC",
        "border-color": "#FB5C78",
        "font-weight": "bold",
      },
    },
    {
      selector: ".update",
      css: {
        "background-color": "#1A1B1F",
        color: "#E8E9EC",
        "border-color": "#60A5FA",
        "font-weight": "bold",
      },
    },
    {
      selector: ".replace",
      css: {
        "background-color": "#1A1B1F",
        color: "#E8E9EC",
        "border-color": "#FBBF24",
        "font-weight": "bold",
      },
    },
    {
      selector: ".no-op",
      css: {
        color: "#B5B8C0",
        "border-opacity": 1,
        "font-weight": "bold",
        "border-width": "3px",
        "border-color": "#272A2E",
        "background-color": "#1A1B1F",
      },
    },
    {
      selector: ".invisible",
      css: {
        opacity: "0",
      },
    },
    {
      selector: ".semitransp",
      css: {
        opacity: "0.4",
      },
    },
    {
      selector: "edge.semitransp",
      css: {
        opacity: "0",
      },
    },
    {
      selector: ".visible",
      css: {
        opacity: "1",
      },
    },
    {
      selector: ".dashed",
      css: {
        "line-style": "dashed",
        "line-dash-pattern": [20, 20],
      },
    },
  ],
};

export default {
  name: "Graph",
  props: {
    // The plan graph payload (nodes + edges), passed from App.
    graph: { type: Object, default: () => ({ nodes: [], edges: [] }) },
    model: { type: Object, default: () => ({}) },
    selectedId: { type: String, default: "" },
    // When true (default) show only changed resources plus the resources
    // connected to them (see #6). When false show everything.
    changedOnly: { type: Boolean, default: true },
    actionFilter: { type: String, default: "all" },
    cbSafe: { type: Boolean, default: false },
  },
  data() {
    return {
      selectedNode: "",
      config,
      guardActive: false,
      noChangeNote: false,
    };
  },
  watch: {
    changedOnly() {
      if (this.graph && this.graph.nodes && this.graph.nodes.length) {
        this.renderGraph();
      }
    },
    graph() {
      if (this.graph && this.graph.nodes && this.graph.nodes.length) {
        this.renderGraph();
      }
    },
  },
  methods: {
    isChanged: function (n) {
      return ["create", "update", "delete", "replace"].includes(n.data.change);
    },
    // The set of node ids to render in changes-only mode: the changed resources,
    // everything transitively downstream of them (the blast radius that a change
    // could affect), their direct (1-hop) upstream dependencies, and the ancestor
    // containers of all of those so the nesting still renders.
    changedSubset: function (changed, edges) {
      const byId = {};
      this.graph.nodes.forEach((n) => {
        byId[n.data.id] = n;
      });

      // source depends on target, so dependents of X are the sources of edges
      // whose target is X, and dependencies of X are the targets of edges whose
      // source is X.
      const dependents = {};
      const dependencies = {};
      edges.forEach((e) => {
        (dependents[e.data.target] = dependents[e.data.target] || []).push(
          e.data.source
        );
        (dependencies[e.data.source] = dependencies[e.data.source] || []).push(
          e.data.target
        );
      });

      const keep = new Set(changed.map((n) => n.data.id));

      // Transitive downstream dependents (blast radius).
      const queue = [...keep];
      while (queue.length) {
        const id = queue.pop();
        (dependents[id] || []).forEach((s) => {
          if (!keep.has(s)) {
            keep.add(s);
            queue.push(s);
          }
        });
      }

      // 1-hop upstream dependencies of the changed resources only.
      changed.forEach((n) => {
        (dependencies[n.data.id] || []).forEach((t) => keep.add(t));
      });

      // Ancestor containers (type/file/module) of everything kept.
      [...keep].forEach((id) => {
        let p = byId[id] && byId[id].data.parent;
        while (p) {
          keep.add(p);
          p = byId[p] && byId[p].data.parent;
        }
      });

      return keep;
    },
    preConfig(cy) {
      cy.use(klay);
      cy.use(svg);

      // Only load nodeHtmlLabel once
      if (typeof cy("core", "nodeHtmlLabel") !== "function") {
        cy.use(nodeHtmlLabel);
      }
    },
    // async afterCreated() {
    //   this.renderGraph();
    // },
    renderGraph: function () {
      let vm = this;
      let cy = this.$refs.cy.instance;
      let el = cy.elements();
      const nodesNames = this.graph.nodes.map((x) => x.data.id);

      // Resolve edge targets first, then add everything in one batch. Adding
      // elements one at a time makes cytoscape recalc style per call, which goes
      // quadratic on big plans and hangs the browser (see #6).
      const edges = [];
      this.graph.edges.forEach((n) => {
        if (n.data.id.includes("-variable") || n.data.id.includes("-output")) {
          return;
        }

        // Browse node ancestors if target is not found in nodes
        // e.g: resource.test.attribute will not be in nodes, as attributes are not exported
        // this ensures that the dependency will be made on resource.test instead
        let name = n.data.target;
        while (!nodesNames.includes(name)) {
          name = name.split(".");
          if (name.length < 2) {
            console.warn("edge target", n.data.target, "not found in nodes");
            return;
          }
          name.pop();
          name = name.join(".");
        }
        n.data.target = name;
        edges.push(n);
      });

      // Decide what to render. By default show only changed resources and the
      // resources connected to them; the toggle brings back the full graph. A
      // plan with no changes falls back to the full graph (nothing to filter to).
      const changed = this.graph.nodes.filter((n) => this.isChanged(n));
      this.noChangeNote = false;
      this.guardActive = false;

      let nodes, renderEdges;
      if (!this.changedOnly || changed.length === 0) {
        nodes = this.graph.nodes;
        renderEdges = edges;
        this.noChangeNote = changed.length === 0;
        this.guardActive = !this.changedOnly && this.graph.nodes.length > 500;
      } else {
        const keep = this.changedSubset(changed, edges);
        nodes = this.graph.nodes.filter((n) => keep.has(n.data.id));
        renderEdges = edges.filter(
          (e) => keep.has(e.data.source) && keep.has(e.data.target)
        );
      }

      cy.batch(() => {
        cy.remove(el);
        cy.add(nodes);
        cy.add(renderEdges);
      });

      // cy.nodeHtmlLabel([
      //   {
      //     query: ".resource-name",
      //     tpl: function (data) {
      //       return `<div class="node ${data.change ? data.change : ""}">${
      //         data.label
      //       }</div>`;
      //     },
      //   },
      //   {
      //     query: ".data-name",
      //     tpl: function (data) {
      //       return `<div class="node data ${data.change ? data.change : ""}">${
      //         data.label
      //       }</div>`;
      //     },
      //   },
      //   {
      //     query: ".variable",
      //     tpl: function (data) {
      //       return `<div class="node variable">${data.label}</div>`;
      //     },
      //   },
      //   {
      //     query: ".output",
      //     tpl: function (data) {
      //       return `<div class="node output">${data.label}</div>`;
      //     },
      //   },
      //   {
      //     query: ".locals",
      //     tpl: function (data) {
      //       return `<div class="node locals">${data.label}</div>`;
      //     },
      //   },
      // ]);

      this.runLayouts();

      // Add click event
      cy.on("click", "node", function (event) {
        var n = event.target;

        let node = { id: n.data().id, in: [], out: [] };
        
        const ce = n.connectedEdges();
        for (let i = 0; i < ce.length; i += 1) {
          let ed = ce[i].data();
          if (n.data().id === ed.source) {
            node.out.push(ed.target);
          } else {
            node.in.push(ed.source);
          }
        }

        // When click on resource group
        let rg = node.id.split("/")[1];
        if (rg) {
          if (rg.endsWith(".tf")) {
            return;
          }
        }

        // When click on directory
        if (["basename", "fname"].includes(n.data().type)) {
          vm.selectedNode = "";
          vm.unhighlightNodePaths(n);
          return;
        } else {
          vm.selectedNode = node.id;
          vm.highlightNodePaths(n);
        }

        vm.$emit("select", node.id);
      });

      // Add hover event
      cy.on("mouseover", "node", function (event) {
        let node = event.target;
        if (!vm.selectedNode) {
          vm.highlightNodePaths(node);
        }
      });
      cy.on("mouseout", "node", function (event) {
        var node = event.target;
        if (!vm.selectedNode) {
          vm.unhighlightNodePaths(node);
        }
      });
    },
    highlightNodePaths: function (node) {
      let cy = this.$refs.cy.instance;
      if (
        !["basename", "fname"].includes(node.data().type) &&
        (!node.isParent() || node.data().type === "module")
      ) {
        // make everything but current node and parent transparent
        cy.elements()
          .difference(node.outgoers().union(node.incomers()))
          .filter(function (e) {
            if (!["basename", "fname"].includes(e.data().type)) {
              return e;
            }
          })
          .not(node.ancestors())
          .addClass("semitransp");

        node
          .neighborhood()
          .union(node.neighborhood().parent())
          .addClass("visible");

        node.incomers().addClass("dashed");
      }
    },
    unhighlightNodePaths: function (node) {
      let cy = this.$refs.cy.instance;
      if (!node.data().type.includes[("basename", "fname")]) {
        cy.elements()
          .removeClass("semitransp")
          .removeClass("visible")
          .removeClass("dashed");
      }
    },
    saveGraph: function () {
      let cy = this.$refs.cy.instance;
      var svgContent = cy.svg({scale: 1, full: true});
			var blob = new Blob([svgContent], {type:"image/svg+xml;charset=utf-8"});
			saveAs(blob, "ponto.svg");

    },
    savePng: function () {
      let cy = this.$refs.cy.instance;
      // full: the whole graph, not just the viewport; bg so it isn't transparent
      var blob = cy.png({output: "blob", full: true, scale: 1, bg: "#ffffff"});
      saveAs(blob, "ponto.png");
    },
    runLayouts: function () {
      let cy = this.$refs.cy.instance;

      cy.layout({
        name: "klay",
        nodeDimensionsIncludeLabels: true,
        klay: {
          direction: "RIGHT",
          borderSpacing: 100,
          spacing: 30,
        },
      }).run();
    },
  },
  mounted() {
    // App passes the graph payload as a prop; render once it has arrived. The
    // graph watcher covers the async case where the prop lands after mount.
    if (this.graph && this.graph.nodes && this.graph.nodes.length) {
      this.renderGraph();
    }
  },
};
</script>

<style>
.graph-canvas {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}

.graph-note {
  margin: 8px;
  padding: 0.4em 0.6em;
  font-size: 0.85em;
  color: #e8e9ec;
  background-color: #2a2410;
  border: 1px solid #4d4413;
  border-radius: 0.25em;
}

#cytoscape-div {
  height: 100% !important;
  min-height: 800px;
  background-color: #15171a !important;
}

.node {
  width: 14em;
  font-size: 2em;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  text-align: center;
  padding: 0.5em 0.5em;
  border-radius: 0.25em;
  background-color: white;
  color: black;
  font-weight: bold;
  cursor: pointer;
  border: 5px solid lightgray;
}

.node:hover {
  transform: scale(1.02);
}

.resource-type {
  width: 20em;
  font-size: 2em;
  height: 100%;
}

.create {
  background-color: #28a745;
  color: white;
  font-weight: bold;
  border: 0;
}

.delete {
  /* background-color: #ffe9e9;
  border: 5px solid #e40707; */
  background-color: #e40707;
  color: white;
  font-weight: bold;
  border: 0;
}

.update {
  /* background-color: #e1f0ff;
  border: 5px solid #1d7ada; */
  background-color: #1d7ada;
  color: white;
  font-weight: bold;
  border: 0;
}

.replace {
  /* background-color: #fff7e0;
  border: 5px solid #ffc107; */
  background-color: #ffc107;
  color: black;
  font-weight: bold;
  border: 0;
}

.output {
  background-color: #fff7e0;
  border: 5px solid #ffc107;
  color: black;
  font-weight: bold;
}

.variable {
  background-color: #e1f0ff;
  border: 5px solid #1d7ada;
  color: black;
  font-weight: bold;
}

.data {
  background-color: #ffecec;
  border: 5px solid #dc477d;
  color: black;
  font-weight: bold;
}

.locals {
  background-color: black;
  color: white;
  font-weight: bold;
  border: 0;
}
</style>


<style scoped>
fieldset {
  margin-bottom: 2em;
}

.graph-enter-active,
.graph-leave-active,
.graph-enter-active legend,
.graph-leave-active legend {
  transition: all 0.2s ease;
  overflow: hidden;
}

.graph-enter,
.graph-leave-to,
.graph-enter legend,
.graph-leave-to legend {
  height: 0;
  padding: 0;
  margin: 0;
  opacity: 0;
}
</style>
