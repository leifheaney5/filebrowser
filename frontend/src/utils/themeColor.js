const HEX_COLOR = /^#(?:[\da-f]{3}|[\da-f]{6})$/i;

function normalizeHexColor(value) {
  if (typeof value !== "string" || !HEX_COLOR.test(value)) {
    return null;
  }

  const hex = value.slice(1).toLowerCase();
  return `#${hex.length === 3 ? [...hex].map((digit) => `${digit}${digit}`).join("") : hex}`;
}

export function themeColorInputValue(value, presets = []) {
  const preset = presets.find((choice) => choice.value === value);
  const presetColor = normalizeHexColor(preset?.hex);
  if (presetColor) {
    return presetColor;
  }

  return normalizeHexColor(value) || normalizeHexColor(presets[0]?.hex) || "#2196f3";
}
