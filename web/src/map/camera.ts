import type { LocationPrecision } from './coordinates'

export type CameraAction = 'initial-fit' | 'selection-fly' | 'none'

export function getInitialCameraAction(initialFitDone: boolean, hasListings: boolean): CameraAction {
  return !initialFitDone && hasListings ? 'initial-fit' : 'none'
}

export function getSelectionCameraAction(previousId: number | null, currentId: number | null, precision: LocationPrecision): CameraAction {
	return currentId != null && currentId !== previousId && precision !== 'UNKNOWN' ? 'selection-fly' : 'none'
}

export function zoomForPrecision(precision: LocationPrecision): number {
	switch (precision) {
	case 'EXACT': return 12
	case 'ADDRESS': return 15
	case 'NEIGHBORHOOD': return 13
	case 'CITY': return 11
	case 'DEPARTMENT': return 8
	case 'COUNTRY': return 5
	default: return 5
	}
}
