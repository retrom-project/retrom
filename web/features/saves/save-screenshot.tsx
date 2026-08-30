import Image from "next/image";

type SaveScreenshotProps = {
  alt: string;
  className?: string;
  height?: number;
  screenshotUrl: string | null | undefined;
  sizes?: string;
  width?: number;
};

export function SaveScreenshot({
  alt,
  className,
  height,
  screenshotUrl,
  sizes,
  width,
}: SaveScreenshotProps) {
  if (!screenshotUrl) {
    return <div className={`save-screenshot-placeholder${className ? ` ${className}` : ""}`} role="img" aria-label={`${alt || "存档截图"}无预览图`}><span>无预览图</span></div>;
  }
  if (width !== undefined && height !== undefined) {
    return <Image className={className} src={screenshotUrl} alt={alt} width={width} height={height} unoptimized />;
  }
  return <Image className={className} src={screenshotUrl} alt={alt} fill sizes={sizes} unoptimized />;
}
