<!-- A spinner that fills its parent, with a note under it while something loads. -->
<script lang="ts">
	let { label }: { label?: string } = $props();
</script>

<div class="flex size-full flex-col items-center justify-center gap-3 text-[12.5px] text-fg-subtle">
	<span class="size-5 animate-spin rounded-full border-2 border-fg-faint border-t-fg" role="status"
	></span>
	{label}
</div>
