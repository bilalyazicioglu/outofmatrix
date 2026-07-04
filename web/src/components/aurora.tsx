/**
 * Fixed full-screen backdrop: three soft radial gradients drifting slowly on
 * their own composited layers. No blur filters — the gradients are already
 * soft — so the effect costs almost nothing to animate.
 */
export function Aurora() {
  return (
    <div aria-hidden className="fixed inset-0 -z-10 overflow-hidden">
      <div
        className="aurora-blob"
        style={{
          top: "-20%",
          left: "-10%",
          width: "60vw",
          height: "60vw",
          background:
            "radial-gradient(circle, oklch(0.45 0.22 295 / 28%) 0%, transparent 65%)",
          animation: "aurora-drift-a 38s ease-in-out infinite",
        }}
      />
      <div
        className="aurora-blob"
        style={{
          bottom: "-25%",
          right: "-15%",
          width: "70vw",
          height: "70vw",
          background:
            "radial-gradient(circle, oklch(0.42 0.2 330 / 22%) 0%, transparent 65%)",
          animation: "aurora-drift-b 46s ease-in-out infinite",
        }}
      />
      <div
        className="aurora-blob"
        style={{
          top: "30%",
          left: "45%",
          width: "45vw",
          height: "45vw",
          background:
            "radial-gradient(circle, oklch(0.5 0.13 220 / 14%) 0%, transparent 65%)",
          animation: "aurora-drift-c 52s ease-in-out infinite",
        }}
      />
      {/* Fine grain so large gradients don't band */}
      <div
        className="absolute inset-0 opacity-[0.035]"
        style={{
          backgroundImage:
            "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='120' height='120'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2'/%3E%3C/filter%3E%3Crect width='120' height='120' filter='url(%23n)'/%3E%3C/svg%3E\")",
        }}
      />
    </div>
  )
}
