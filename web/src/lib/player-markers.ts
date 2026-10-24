export const playerMarkers = [
  { id: 'circle', symbol: '●', name: '圆点' },
  { id: 'triangle', symbol: '▲', name: '三角' },
  { id: 'diamond', symbol: '◆', name: '菱形' },
  { id: 'square', symbol: '■', name: '方块' },
] as const;

export function playerMarker(index: number) {
  return playerMarkers[((index % playerMarkers.length) + playerMarkers.length) % playerMarkers.length];
}
