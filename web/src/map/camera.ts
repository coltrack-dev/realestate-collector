export type CameraAction = 'initial-fit' | 'selection-fly' | 'none'

export function getInitialCameraAction(initialFitDone: boolean, hasListings: boolean): CameraAction {
  return !initialFitDone && hasListings ? 'initial-fit' : 'none'
}

export function getSelectionCameraAction(previousId: number | null, currentId: number | null, hasValidCoordinate: boolean): CameraAction {
  return currentId != null && currentId !== previousId && hasValidCoordinate ? 'selection-fly' : 'none'
}
