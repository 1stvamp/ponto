<template>
  <div class="titlebar">
    <div class="tb-left">
      <span class="mark"></span>
      <span class="name">Ponto</span>
      <span class="tagline">Terraform Visualizer</span>
    </div>

    <div class="tb-center">
      <span
        v-for="c in summaryChips"
        :key="c.label"
        class="chip"
        :class="{ zero: c.count === 0 }"
      >
        <span class="chip-glyph" :style="{ color: c.count === 0 ? '#5F6570' : c.color }">{{ c.glyph }}</span>
        <span class="chip-count">{{ c.count }}</span>
        <span class="chip-label">{{ c.label }}</span>
      </span>
    </div>

    <div class="tb-right">
      <button
        class="cb-btn"
        :class="{ on: cbSafe }"
        title="Colour-blind-safe palette"
        @click="$emit('toggleCb')"
      >
        <span class="cb-icon">◑</span> CB-safe
      </button>
      <div class="export">
        <button id="exportToggle" class="export-btn" @click="exportOpen = !exportOpen">Export ▾</button>
        <div v-if="exportOpen" class="export-menu" @mouseleave="exportOpen = false">
          <button id="saveGraph" @click="pick('saveGraph')">Save graph as SVG</button>
          <button id="savePng" @click="pick('savePng')">Save graph as PNG</button>
          <button @click="pick('copySummary')">Copy plan summary</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
export default {
  name: "MainNav",
  props: {
    counts: { type: Object, default: () => ({}) },
    cbSafe: Boolean,
    cbColor: { type: Function, default: () => "#fff" },
    glyph: { type: Function, default: () => "·" },
  },
  data() {
    return { exportOpen: false };
  },
  computed: {
    summaryChips() {
      const defs = [
        ["create", "to add"],
        ["update", "to change"],
        ["delete", "to destroy"],
        ["replace", "to replace"],
      ];
      return defs.map(([action, label]) => ({
        label,
        count: this.counts[action] || 0,
        glyph: this.glyph(action),
        color: this.cbColor(action),
      }));
    },
  },
  methods: {
    pick(evt) {
      this.exportOpen = false;
      this.$emit(evt);
    },
  },
};
</script>

<style scoped>
.titlebar {
  height: 48px;
  flex: 0 0 48px;
  display: flex;
  align-items: center;
  padding: 0 14px;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border);
}

.tb-left {
  display: flex;
  align-items: center;
  gap: 8px;
}
.mark {
  width: 20px;
  height: 20px;
  border-radius: 5px;
  background: linear-gradient(135deg, #a8ff53, #28bf5c);
  position: relative;
}
.mark::after {
  content: "";
  position: absolute;
  width: 8px;
  height: 8px;
  background: #15171a;
  border-radius: 2px;
  top: 6px;
  left: 6px;
}
.name {
  font-weight: 600;
  font-size: 14px;
  color: var(--text);
}
.tagline {
  font-size: 12px;
  color: var(--text-faint);
}

.tb-center {
  flex: 1 1 auto;
  display: flex;
  justify-content: center;
  gap: 14px;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
}
.chip-glyph {
  font-family: var(--mono);
  font-weight: 600;
}
.chip-count {
  color: var(--text);
  font-family: var(--mono);
}
.chip-label {
  color: var(--text-dim);
}
.chip.zero .chip-count,
.chip.zero .chip-label {
  color: var(--text-faint);
}

.tb-right {
  display: flex;
  align-items: center;
  gap: 8px;
}
.cb-btn {
  height: 30px;
  padding: 0 10px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: transparent;
  color: var(--text-def);
  border: 1px solid var(--border);
  border-radius: 3px;
  cursor: pointer;
  font-size: 12px;
}
.cb-btn.on {
  background: var(--bg-sel);
  border-color: #3b3e45;
  color: var(--text);
}
.cb-icon {
  font-size: 13px;
}

.export {
  position: relative;
}
.export-btn {
  height: 30px;
  padding: 0 12px;
  background: #2c3034;
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 3px;
  cursor: pointer;
  font-size: 12px;
}
.export-menu {
  position: absolute;
  top: 34px;
  right: 0;
  min-width: 180px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 4px;
  z-index: 40;
  display: flex;
  flex-direction: column;
}
.export-menu button {
  text-align: left;
  padding: 7px 10px;
  background: transparent;
  border: 0;
  border-radius: 4px;
  color: var(--text-def);
  cursor: pointer;
  font-size: 12px;
}
.export-menu button:hover {
  background: var(--bg-sel);
  color: var(--text);
}
</style>
