interface BrandMarkProps {
  size: number
  className?: string
}

export function BrandMark({ size, className }: BrandMarkProps) {
  return <svg data-testid="flowlens-brand-mark" className={className} width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
    <rect x="1" y="1" width="30" height="30" rx="7" fill="#20312f" stroke="#3e5a57" strokeWidth="2" />
    <path d="M4 16h3l2.5-8 4 16 3.2-11 2.3 6H28" fill="none" stroke="#62d0a2" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
}
