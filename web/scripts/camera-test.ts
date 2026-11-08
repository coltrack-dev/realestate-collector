import assert from 'node:assert/strict'
import { getInitialCameraAction, getSelectionCameraAction } from '../src/map/camera.ts'

assert.equal(getInitialCameraAction(false, true), 'initial-fit')
assert.equal(getInitialCameraAction(true, true), 'none')
assert.equal(getInitialCameraAction(false, false), 'none')

// A listing/filter update after the initial fit has no camera action.
assert.equal(getInitialCameraAction(true, true), 'none')
assert.equal(getSelectionCameraAction(null, 10, 'EXACT'), 'selection-fly')
assert.equal(getSelectionCameraAction(10, 10, 'EXACT'), 'none')
assert.equal(getSelectionCameraAction(null, 11, 'UNKNOWN'), 'none')
assert.equal(getSelectionCameraAction(11, null, 'CITY'), 'none')
console.log('camera tests passed')
