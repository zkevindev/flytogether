import { playerMarker } from '../lib/player-markers';

export function PlayerMarker({ index, className = '' }: { index: number; className?: string }) {
  const marker = playerMarker(index);
  return <span className={`player-marker player-marker-${marker.id} ${className}`.trim()} aria-hidden="true">{marker.symbol}</span>;
}
