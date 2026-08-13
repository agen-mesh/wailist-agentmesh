import { ImageResponse } from "next/og";

export const alt = "AgentMesh — design, deploy, and monitor AI agent workflows.";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default function OpengraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "center",
          alignItems: "flex-start",
          padding: "80px",
          background: "#08070c",
          backgroundImage:
            "radial-gradient(circle at 78% 30%, rgba(167,140,250,0.35), transparent 55%), radial-gradient(circle at 15% 85%, rgba(139,92,246,0.25), transparent 50%)",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: "16px",
            marginBottom: "40px",
          }}
        >
          <div
            style={{
              width: "56px",
              height: "56px",
              borderRadius: "14px",
              background: "linear-gradient(135deg, #6366f1 0%, #a855f7 60%, #fcd34d 100%)",
              display: "flex",
            }}
          />
          <div style={{ fontSize: 34, color: "#8e8aa3", fontWeight: 600, display: "flex" }}>
            AgentMesh
          </div>
        </div>
        <div
          style={{
            fontSize: 64,
            fontWeight: 700,
            color: "#f2f0f7",
            lineHeight: 1.15,
            display: "flex",
            maxWidth: "920px",
          }}
        >
          Design, deploy, and monitor AI agent workflows
        </div>
        <div
          style={{
            marginTop: "32px",
            fontSize: 28,
            color: "#a78bfa",
            display: "flex",
          }}
        >
          agent-mesh.app
        </div>
      </div>
    ),
    { ...size }
  );
}
