<template>
  <div class="theme-color-picker">
    <ButtonGroup
      :buttons="presets"
      :initial-active="modelValue"
      :is-disabled="disabled"
      @button-clicked="selectColor"
    />
    <label class="custom-color-choice">
      <input
        type="color"
        :value="inputValue"
        :disabled="disabled"
        :aria-label="label"
        @change="selectCustomColor"
      />
      <span>{{ inputValue.toUpperCase() }}</span>
    </label>
  </div>
</template>

<script>
import ButtonGroup from "@/components/ButtonGroup.vue";
import { themeColorInputValue } from "@/utils/themeColor.js";

export default {
  name: "ThemeColorPicker",
  components: { ButtonGroup },
  props: {
    modelValue: { type: String, default: "" },
    presets: { type: Array, default: () => [] },
    disabled: { type: Boolean, default: false },
    label: { type: String, required: true },
  },
  emits: ["update:modelValue"],
  computed: {
    inputValue() {
      return themeColorInputValue(this.modelValue, this.presets);
    },
  },
  methods: {
    selectColor(color) {
      if (!this.disabled) {
        this.$emit("update:modelValue", color);
      }
    },
    selectCustomColor(event) {
      this.selectColor(event.target.value.toLowerCase());
    },
  },
};
</script>

<style scoped>
.theme-color-picker {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.theme-color-picker :deep(.button-group) {
  flex: 1 1 22rem;
  margin: 0;
}

.custom-color-choice {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  min-height: 2.75rem;
  padding: 0.25rem 0.65rem 0.25rem 0.35rem;
  border: 1px solid var(--surfaceSecondary);
  border-radius: var(--borderRadius, 6px);
  color: var(--textPrimary);
  font: inherit;
  font-size: 0.8rem;
}

.custom-color-choice input[type="color"] {
  width: 2.25rem;
  height: 2.25rem;
  padding: 0.15rem;
  border: 0;
  border-radius: 4px;
  background: transparent;
  cursor: pointer;
}

.custom-color-choice input[type="color"]:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.custom-color-choice span {
  font-variant-numeric: tabular-nums;
}

@media (width <= 520px) {
  .theme-color-picker :deep(.button-group) {
    flex-basis: 100%;
  }
}
</style>
