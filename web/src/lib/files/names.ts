/** Why a name can't be used for a file or folder, or null if it can. */
export function nameProblem(name: string): string | null {
	if (name.includes('/')) {
		return 'Names can’t contain “/”';
	}
	if (name === '.' || name === '..') {
		return 'That name is reserved';
	}
	if (name.includes('\0')) {
		return 'Names can’t contain a NUL';
	}
	return null;
}
