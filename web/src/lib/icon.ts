import type { Component } from 'svelte';
import type { ClassValue } from 'svelte/elements';

/** An icon component, such as one from @lucide/svelte. */
export type Icon = Component<{ class?: ClassValue; strokeWidth?: number | string }>;
