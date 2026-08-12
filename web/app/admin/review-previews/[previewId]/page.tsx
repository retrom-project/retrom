import { ReviewPreviewPlayer } from "@/features/reviews/review-preview-player";

export default async function ReviewPreviewPage({ params }: { params: Promise<{ previewId: string }> }) {
  const { previewId } = await params;
  return <ReviewPreviewPlayer previewId={previewId} />;
}
