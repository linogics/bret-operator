// Magenta overrides for the d2 themes, prepended to every d2 code block.
// See https://d2lang.com/tour/themes for the color codes.
const vars = `vars: {
  d2-config: {
    theme-overrides: {
      N7: "#ffffff"
      B1: "#6b0f45"
      B2: "#b5207a"
      B3: "#e04d9f"
      B4: "#f7c6e1"
      B5: "#fbe0ef"
      B6: "#fdf2f8"
      AA2: "#b5207a"
      AA4: "#f7c6e1"
      AA5: "#fbe0ef"
      AB4: "#f7c6e1"
      AB5: "#fbe0ef"
    }
    dark-theme-overrides: {
      N1: "#eceef2"
      N7: "#17181c"
      B1: "#f9bfdf"
      B2: "#ec6db5"
      B3: "#8a3a68"
      B4: "#5c2446"
      B5: "#451a35"
      B6: "#2e1424"
      AA2: "#ec6db5"
      AA4: "#5c2446"
      AA5: "#451a35"
      AB4: "#5c2446"
      AB5: "#451a35"
    }
  }
}
`;

// Must be listed before astro-d2 so the vars are in place when d2 renders.
export default function d2Theme() {
	return {
		name: 'd2-theme',
		hooks: {
			'astro:config:setup': ({ config }) => {
				config.markdown.processor.options.mdastPlugins ??= [];
				config.markdown.processor.options.mdastPlugins.push(() => ({
					name: 'd2-theme',
					code(node) {
						if (node.lang === 'd2') return { ...node, value: vars + node.value };
					},
				}));
			},
		},
	};
}
