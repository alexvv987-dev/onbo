"""Assemble artifact-tool slide renders when its PDF font exporter is unavailable."""
from pathlib import Path
from reportlab.pdfgen import canvas
from reportlab.lib.utils import ImageReader

root = Path(__file__).resolve().parent.parent
pages = sorted((root / "tmp/slides").glob("slide-*.png"), key=lambda path: int(path.stem.split("-")[-1]))
if len(pages) != 6:
    raise RuntimeError("Expected six reviewed slide renders")
target = root / "output/pdf/onbo-mvp.pdf"
target.parent.mkdir(parents=True, exist_ok=True)
pdf = canvas.Canvas(str(target), pagesize=(960, 540))
pdf.setTitle("Онбо — MVP AI-buddy")
pdf.setAuthor("Onbo")
pdf.setSubject("План адаптации, ответы по инструкциям и подтверждаемая заявка на доступ")
for index, page in enumerate(pages):
    pdf.drawImage(ImageReader(str(page)), 0, 0, width=960, height=540)
    if index == 0:
        pdf.linkURL("https://github.com/alexvv987-dev/onbo", (54, 74, 870, 96), relative=0, thickness=0)
        pdf.linkURL("../video/onbo-gigachat.webm", (54, 54, 870, 74), relative=1, thickness=0)
    pdf.showPage()
pdf.save()
print("PDF assembled from six rendered slides")
