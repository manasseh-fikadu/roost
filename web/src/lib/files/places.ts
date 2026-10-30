import AppWindow from '@lucide/svelte/icons/app-window';
import FolderClock from '@lucide/svelte/icons/folder-clock';
import HardDrive from '@lucide/svelte/icons/hard-drive';
import House from '@lucide/svelte/icons/house';
import Package from '@lucide/svelte/icons/package';
import ScrollText from '@lucide/svelte/icons/scroll-text';
import Server from '@lucide/svelte/icons/server';
import Settings from '@lucide/svelte/icons/settings';
import Users from '@lucide/svelte/icons/users';
import type { Icon } from '#lib/icon.js';

export interface Place {
	path: string;
	label: string;
	icon: Icon;
}

/** The folders worth a shortcut on a machine, by its OS. */
export function quickPlaces(os: string, home: string | null): Place[] {
	const common: Place[] = [
		{ path: home ?? '~', label: 'Home', icon: House },
		{ path: '/', label: 'Root', icon: HardDrive }
	];
	if (os === 'macOS') {
		return [
			...common,
			{ path: '/Applications', label: 'Applications', icon: AppWindow },
			{ path: '/Users', label: '/Users', icon: Users },
			{ path: '/Volumes', label: '/Volumes', icon: HardDrive },
			{ path: '/tmp', label: '/tmp', icon: FolderClock },
			{ path: '/var/log', label: '/var/log', icon: ScrollText }
		];
	}
	return [
		...common,
		{ path: '/tmp', label: '/tmp', icon: FolderClock },
		{ path: '/var/log', label: '/var/log', icon: ScrollText },
		{ path: '/etc', label: '/etc', icon: Settings },
		{ path: '/srv', label: '/srv', icon: Server },
		{ path: '/opt', label: '/opt', icon: Package }
	];
}
