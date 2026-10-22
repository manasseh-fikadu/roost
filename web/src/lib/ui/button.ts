// Button styles, shared by <Button> and by links and bits-ui triggers that
// should look like buttons.

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';
export type ButtonSize = 'xs' | 'sm' | 'md';

const base =
	'inline-flex shrink-0 items-center justify-center font-medium whitespace-nowrap transition-colors duration-150 disabled:pointer-events-none disabled:opacity-50 aria-disabled:pointer-events-none aria-disabled:opacity-50';

const variants: Record<ButtonVariant, string> = {
	primary: 'border border-transparent bg-fg text-canvas hover:bg-fg/80',
	secondary: 'border border-line-strong bg-surface text-fg hover:border-line-heavy hover:bg-hover',
	ghost: 'border border-transparent bg-transparent text-fg-muted hover:bg-hover hover:text-fg',
	danger: 'border border-transparent bg-red text-white hover:bg-red/85'
};

const sizes: Record<ButtonSize, string> = {
	xs: 'h-6 gap-1 rounded-md px-2 text-[12px]',
	sm: 'h-8 gap-1.5 rounded-md px-3 text-[13px]',
	md: 'h-10 gap-2 rounded-lg px-4 text-[14px]'
};

/** The classes of a button. */
export function buttonClass(variant: ButtonVariant = 'secondary', size: ButtonSize = 'sm'): string {
	return `${base} ${sizes[size]} ${variants[variant]}`;
}

export type IconButtonSize = 'sm' | 'md';

const iconSizes: Record<IconButtonSize, string> = { sm: 'size-7', md: 'size-8' };

/** The classes of a square button that holds only an icon. */
export function iconButtonClass(size: IconButtonSize = 'md'): string {
	return `inline-flex shrink-0 items-center justify-center rounded-md text-fg-subtle transition-colors hover:bg-hover hover:text-fg disabled:pointer-events-none disabled:opacity-50 ${iconSizes[size]}`;
}

/** The classes of a text field. */
export const fieldClass =
	'w-full rounded-md border border-line-strong bg-canvas px-3 text-[14px] text-fg transition-colors placeholder:text-fg-faint hover:border-line-heavy focus:border-fg-subtle focus:outline-none disabled:opacity-50';
