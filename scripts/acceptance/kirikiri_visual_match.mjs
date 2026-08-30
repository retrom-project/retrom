const minimumDiscriminativePixels = 100;
const discriminativeRgbDistance = 48;

export function compareKiriKiriVisualSamples(bFrame, cFrame, restoredFrame) {
  if (!validSample(bFrame) || !validSample(cFrame) || !validSample(restoredFrame) ||
      bFrame.length !== cFrame.length || bFrame.length !== restoredFrame.length) {
    throw new Error("KIRIKIRI_ACCEPTANCE_VISUAL_SAMPLE_INVALID");
  }
  let discriminativePixelCount = 0;
  let restoredToBDistance = 0;
  let restoredToCDistance = 0;
  for (let offset = 0; offset < bFrame.length; offset += 4) {
    const bToC = rgbDistance(bFrame, cFrame, offset);
    if (bToC < discriminativeRgbDistance) {continue;}
    discriminativePixelCount += 1;
    restoredToBDistance += rgbDistance(restoredFrame, bFrame, offset);
    restoredToCDistance += rgbDistance(restoredFrame, cFrame, offset);
  }
  const restoredToBMeanDistance = meanDistance(restoredToBDistance, discriminativePixelCount);
  const restoredToCMeanDistance = meanDistance(restoredToCDistance, discriminativePixelCount);
  return {
    discriminativePixelCount,
    matched: discriminativePixelCount >= minimumDiscriminativePixels &&
      restoredToBMeanDistance * 2 < restoredToCMeanDistance,
    restoredToBMeanDistance,
    restoredToCMeanDistance,
  };
}

function rgbDistance(left, right, offset) {
  return Math.abs(left[offset] - right[offset]) +
    Math.abs(left[offset + 1] - right[offset + 1]) +
    Math.abs(left[offset + 2] - right[offset + 2]);
}

function meanDistance(total, count) {
  return count ? Math.round(total * 1_000 / count) / 1_000 : 0;
}

function validSample(value) {
  return Array.isArray(value) && value.length >= minimumDiscriminativePixels * 4 && value.length % 4 === 0 &&
    value.every((component) => Number.isInteger(component) && component >= 0 && component <= 255);
}
