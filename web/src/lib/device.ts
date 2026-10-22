// How the UI shows a device.

import Cpu from '@lucide/svelte/icons/cpu';
import Laptop from '@lucide/svelte/icons/laptop';
import Server from '@lucide/svelte/icons/server';
import Smartphone from '@lucide/svelte/icons/smartphone';
import type { Device } from './api/types.js';
import type { Status } from './status.js';
import type { Icon } from './icon.js';

/** An icon for a device, from its operating system. */
export function deviceIcon(d: Device): Icon {
	switch (d.os.toLowerCase()) {
		case 'ios':
		case 'android':
			return Smartphone;
		case 'macos':
		case 'windows':
			return Laptop;
		case 'linux':
		case 'freebsd':
		case 'openbsd':
			return Server;
		default:
			return Cpu;
	}
}

/** Whether a device is online, as a Status. */
export function deviceStatus(d: Device): Status {
	return d.online ? 'online' : 'offline';
}

/** The first part of a device's DNS name, such as laptop for laptop.example.ts.net. */
export function shortName(d: Device): string {
	return d.dnsName.split('.')[0];
}
