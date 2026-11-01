// What an admin can do to a container, and what Bield says before it does it.

import Play from '@lucide/svelte/icons/play';
import RotateCw from '@lucide/svelte/icons/rotate-cw';
import Square from '@lucide/svelte/icons/square';
import type { Component } from 'svelte';
import type { DockerAction } from './api.js';

/** An action waiting for an admin to confirm it. */
export interface PendingAction {
	/** The container's ID and name, and the state it was in. */
	id: string;
	name: string;
	state: string;
	action: DockerAction;
}

interface ActionCopy {
	/** The action as a verb for a button, such as "Stop". */
	label: string;
	/** What the dialog says it does. */
	body: string;
	/** What the activity log says, and the toast, once it's done: "Stopped". */
	done: string;
}

export const actionCopy: Record<DockerAction, ActionCopy> = {
	start: {
		label: 'Start',
		body: 'Starts the container again with its existing configuration.',
		done: 'Started'
	},
	stop: {
		label: 'Stop',
		body: 'Docker sends SIGTERM, and kills the container if it still runs after 10 seconds. It stays stopped until it is started again.',
		done: 'Stopped'
	},
	restart: {
		label: 'Restart',
		body: 'Stops the container (SIGTERM, then SIGKILL after 10 seconds) and starts it again. Anything it serves is briefly unavailable.',
		done: 'Restarted'
	}
};

/** The icon of each action. */
export const actionIcons: Record<DockerAction, Component> = {
	start: Play,
	stop: Square,
	restart: RotateCw
};
