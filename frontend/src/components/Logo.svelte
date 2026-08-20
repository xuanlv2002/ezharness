<script lang="ts">
  /* ez 系列统一 logo（源自 ezloop/docs/logo.svg：光子轨道）。 */
  let { size = 24 }: { size?: number } = $props()
</script>

<svg
  viewBox="0 0 200 200"
  width={size}
  height={size}
  role="img"
  aria-label="ezharness logo"
>
  <defs>
    <linearGradient id="ez-ring-grad" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#0ea5e9" />
      <stop offset=".5" stop-color="#00add8" />
      <stop offset="1" stop-color="#8b5cf6" />
    </linearGradient>
  </defs>
  <g>
    <circle
      cx="100"
      cy="100"
      r="60"
      fill="none"
      stroke="url(#ez-ring-grad)"
      stroke-width="4.5"
      stroke-linecap="round"
      stroke-dasharray="290 87"
      transform="rotate(-35 100 100)"
    />
    <circle cx="146" cy="61" r="6.5" fill="#0369a1" />
    <animateTransform
      attributeName="transform"
      type="rotate"
      from="0 100 100"
      to="360 100 100"
      dur="16s"
      repeatCount="indefinite"
    />
  </g>
  <circle cx="100" cy="100" r="8" fill="#0c4a6e" />
</svg>
