<!-- A small line of recent values with no axes, as on a metric tile. -->
<script lang="ts">
	import { areaPath, linePath, runs } from './path.js';

	interface Props {
		/** Evenly spaced values, oldest first. Null breaks the line. */
		values: (number | null)[];
		height?: number;
		/** A CSS color. */
		color?: string;
		/** A fading area under the line. */
		fill?: boolean;
		/** The value at the bottom. The smallest value if it's not given. */
		min?: number;
		/** The value at the top. The largest value if it's not given. */
		max?: number;
		class?: string;
	}

	let {
		values,
		height = 28,
		color = 'var(--color-fg)',
		fill = true,
		min,
		max,
		class: className
	}: Props = $props();

	const gradient = $props.id();
	let width = $state(0);

	const lines = $derived.by(() => {
		const nums = values.filter((v) => v !== null);
		const lo = min ?? (nums.length ? Math.min(...nums) : 0);
		const hi = max ?? (nums.length ? Math.max(...nums) : 1);
		const span = hi - lo || 1;
		const x = (i: number) => (values.length > 1 ? (i / (values.length - 1)) * width : width / 2);
		const y = (v: number) => height - 2 - ((v - lo) / span) * (height - 4);
		return runs(
			values.map((v, i) => ({ ts: i, v })),
			x,
			y,
			1
		);
	});
</script>

<div
	bind:clientWidth={width}
	class={['w-full', className]}
	style:height="{height}px"
	aria-hidden="true"
>
	{#if width > 0}
		<svg {width} {height} class="block overflow-visible">
			<defs>
				<linearGradient id={gradient} x1="0" x2="0" y1="0" y2="1">
					<stop offset="0%" stop-color={color} stop-opacity="0.22" />
					<stop offset="100%" stop-color={color} stop-opacity="0" />
				</linearGradient>
			</defs>
			{#each lines as run, i (i)}
				{#if fill}
					<path d={areaPath(run, height)} fill="url(#{gradient})" />
				{/if}
				<path
					d={linePath(run)}
					fill="none"
					stroke={color}
					stroke-width="1.25"
					stroke-linejoin="round"
					stroke-linecap="round"
				/>
			{/each}
		</svg>
	{/if}
</div>
