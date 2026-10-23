<!-- A score out of 100 as a ring that fills, green at 90 and up, amber at 70, red below. -->
<script lang="ts">
	import { scoreColor } from '#lib/checks.js';

	interface Props {
		/** The score, or null if there isn't one yet. */
		score: number | null;
		/** The width and height, in pixels. */
		size?: number;
	}

	let { score, size = 56 }: Props = $props();

	const stroke = 4;
	const r = $derived((size - stroke) / 2);
	const circumference = $derived(2 * Math.PI * r);
	const color = $derived(scoreColor(score));
</script>

<div
	class="relative shrink-0"
	style:width="{size}px"
	style:height="{size}px"
	role="img"
	aria-label={score === null ? 'No health score yet' : `Health score ${score} out of 100`}
>
	<svg width={size} height={size} class="-rotate-90" aria-hidden="true">
		<circle
			cx={size / 2}
			cy={size / 2}
			{r}
			fill="none"
			stroke="var(--color-line)"
			stroke-width={stroke}
		/>
		<circle
			cx={size / 2}
			cy={size / 2}
			{r}
			fill="none"
			stroke={color}
			stroke-width={stroke}
			stroke-linecap="round"
			stroke-dasharray={circumference}
			stroke-dashoffset={circumference * (1 - (score ?? 0) / 100)}
			class="transition-[stroke-dashoffset] duration-700"
		/>
	</svg>
	<div
		class="absolute inset-0 flex items-center justify-center num text-[16px] font-semibold text-fg"
		aria-hidden="true"
	>
		{score ?? '—'}
	</div>
</div>
