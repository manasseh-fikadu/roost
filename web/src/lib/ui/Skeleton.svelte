<!-- A shimmering block that holds a place while something loads. Size it with classes. -->
<script lang="ts">
	interface Props {
		class?: string;
	}

	let { class: className }: Props = $props();
</script>

<div class={['animate-shimmer rounded-md bg-hover', className]} aria-hidden="true"></div>
