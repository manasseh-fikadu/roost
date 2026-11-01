<!-- Bield's mark: three linked nodes. -->
<script lang="ts">
	interface Props {
		class?: string;
	}

	let { class: className = 'size-5' }: Props = $props();
</script>

<svg viewBox="0 0 24 24" class={className} fill="none" aria-hidden="true">
	<circle cx="12" cy="5" r="2.6" fill="currentColor" />
	<circle cx="5" cy="18" r="2.6" fill="currentColor" />
	<circle cx="19" cy="18" r="2.6" fill="currentColor" />
	<path
		d="M12 7.6V11m0 0-5.2 5M12 11l5.2 5"
		stroke="currentColor"
		stroke-width="1.6"
		stroke-linecap="round"
	/>
</svg>
