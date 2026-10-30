// Where a container's page is.

import { resolve } from '$app/paths';

/** The link to a container's page. The device says whose Docker it is. */
export function containerHref(deviceId: string, name: string): string {
	return `${resolve('/docker/[name]', { name })}?device=${encodeURIComponent(deviceId)}`;
}
