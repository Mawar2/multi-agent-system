import React, { useState } from "react";

const TEAL = "#1D9E75";
const BLUE = "#378ADD";
const AMBER = "#EF9F27";
const RED = "#E24B4A";
const PURPLE = "#7F77DD";
const GREEN = "#639922";
const INK = "#0C1322";
const PANEL = "#141B2A";
const PANEL2 = "#1B2436";
const LINE = "#2B3550";
const MUTE = "#8A98B2";

const DETAILS = {
  hunter: { title: "Hunter Agent", zone: "Zone 1 · Scheduled", color: BLUE, body: "Runs daily. Queries the SAM.gov Opportunities API filtered by BlueMeta's NAICS codes, maps each result into an Opportunity object. Decoupled from the live API via a cached mode so the system builds without a key.", io: "IN: SAM.gov API -> OUT: Opportunity[]" },
  scorer: { title: "Scorer Agent", zone: "Zone 1 · Scheduled", color: BLUE, body: "Reads each filtered opportunity, extracts must-have requirements, evaluates fit against BlueMeta's capability profile, assigns a 0-100 score with explainable reasoning and a BID / NO_BID / REVIEW recommendation. The innovation core.", io: "IN: Opportunity[] -> OUT: scored Opportunity[] -> queue" },
  queue: { title: "Opportunity Queue", zone: "The bridge", color: TEAL, body: "Shared store of scored, vetted opportunities - the dashboard the human team sorts by deadline and fit. The boundary between zones. Nothing here is 'managed' yet; these are candidates awaiting selection.", io: "Scored opportunities, ranked, awaiting selection" },
  trigger: { title: "Selection Event", zone: "Zone 1 -> Zone 2", color: AMBER, body: "A human (or auto-rule: BID + deadline soon) selects an opportunity to pursue. The ONLY bridge between zones - flips status to 'selected' and spins up a dedicated Manager for that proposal.", io: "Selecting one opportunity -> spawns one Manager" },
  manager: { title: "Manager Agent (one per proposal)", zone: "Zone 2 · Orchestrated", color: AMBER, body: "The contract lead for a single proposal. Feeds each sub-agent its context, confirms each step finished before triggering the next, pauses at the human gate (native ADK RequireConfirmation), resumes on approval. It coordinates - it does not write.", io: "Orchestrates Outline -> Writer -> Gate -> Final Review" },
  outline: { title: "Outline Agent", zone: "Zone 2 · Orchestrated", color: BLUE, body: "Generates the proposal skeleton: required sections plus government formatting rules (fonts, margins, mandatory artifacts). Self-contained, no RAG. Output lives in Google Drive.", io: "IN: Opportunity -> OUT: proposal outline (Drive)" },
  writer: { title: "Technical Writer Agent", zone: "Zone 2 · Orchestrated", color: BLUE, body: "Fills sections from BlueMeta's past-performance knowledge base. Flags gaps where we lack past performance ('hard cybersecurity requirement - needs a partner') instead of bluffing. Most build-intensive agent.", io: "IN: outline + KB -> OUT: first draft + gap flags" },
  gate: { title: "Human Review Gate", zone: "Zone 2 · The one manual step", color: RED, body: "The deliberate human-in-the-loop checkpoint. A person validates accuracy, confirms must-haves are addressed, handles flagged gaps. We never let AI promise the government things we can't deliver. Manager pauses here, notifies via Slack/email.", io: "Manager pauses · waits for approval · resumes" },
  final: { title: "Final Review Agent", zone: "Zone 2 · Orchestrated", color: BLUE, body: "After approval: final QA for consistency, compliance, branding, grammar, formatting against the solicitation spec. Once it clears, a human submits to the government.", io: "IN: approved draft -> OUT: submission-ready proposal" },
};

const PHASES = [
  { n: 0, name: "Foundation + Hunter", accent: GREEN, nodes: ["hunter", "queue"], gcp: ["Vertex AI API (enable)", "Cloud project + IAM", "Secret Manager"], data: ["Opportunity schema (shared struct)", "SAM.gov client interface (live/cached)", "Queue as Store interface - JSON file"], note: "Provision lazily, design eagerly. Queue is an interface, not a DB yet." },
  { n: 1, name: "Scorer + real queue", accent: TEAL, nodes: ["hunter", "scorer", "queue"], gcp: ["Gemini 3 Pro (Vertex AI)", "Firestore", "Cloud Scheduler", "Cloud Run"], data: ["Queue -> Firestore (same interface)", "Dashboard read interface (status/deadline/score)"], note: "First real database. LLM spend begins here." },
  { n: 2, name: "Manager + orchestration", accent: AMBER, nodes: ["trigger", "manager", "outline"], gcp: ["Vertex AI Agent Engine", "Vertex AI Sessions / state service"], data: ["AgentResult contract (finalized)", "Selection-event interface (zone bridge)", "Session-state handoff layer", "Human-gate notify + resume interface"], note: "Agent Engine earns its place: per-proposal Managers, concurrency becomes the platform's job." },
  { n: 3, name: "Writer + knowledge base", accent: BLUE, nodes: ["outline", "writer", "final"], gcp: ["Google Drive API", "Vertex AI Vector Search / RAG", "Cloud Storage", "Slack / email integration"], data: ["Past-performance knowledge base (corpus -> embed -> query)", "Drive document interface"], note: "Hidden critical path. Heaviest data layer - start organizing the corpus NOW." },
  { n: 4, name: "Memory + scale hardening", accent: PURPLE, nodes: ["manager"], gcp: ["Vertex AI Memory Bank (Preview)", "Cloud Logging + tracing"], data: ["Cross-proposal memory (sharpens scoring)", "Observability for N concurrent Managers"], note: "Production hardening for real daily operation at scale." },
];

function Node({ id, color, title, sub, w, active, dimmed, onClick }) {
  return (
    <div onClick={() => onClick(id)} style={{ width: w, cursor: "pointer", background: active ? color : PANEL2, border: "1px solid " + (active ? color : LINE), borderRadius: 10, padding: "11px 13px", transition: "all .18s ease", opacity: dimmed ? 0.32 : 1, boxShadow: active ? "0 8px 24px -10px " + color : "none" }}>
      <div style={{ fontSize: 13.5, fontWeight: 600, color: active ? INK : "#E7ECF6" }}>{title}</div>
      <div style={{ fontSize: 11, color: active ? INK : MUTE, marginTop: 2, opacity: active ? 0.8 : 1 }}>{sub}</div>
    </div>
  );
}
const Arrow = ({ vertical }) => (<div style={{ display: "flex", alignItems: "center", justifyContent: "center", padding: vertical ? "2px 0" : "0 4px" }}><span style={{ fontSize: 16, color: "#42506e" }}>{vertical ? "\u2193" : "\u2192"}</span></div>);

function App() {
  const [sel, setSel] = useState("trigger");
  const [phaseIdx, setPhaseIdx] = useState(0);
  const [cumulative, setCumulative] = useState(true);
  const d = DETAILS[sel];
  const phase = PHASES[phaseIdx];

  const activeNodes = cumulative
    ? new Set(PHASES.slice(0, phaseIdx + 1).flatMap((p) => p.nodes))
    : new Set(phase.nodes);
  const isLive = (id) => activeNodes.has(id);
  const shownPhases = cumulative ? PHASES.slice(0, phaseIdx + 1) : [phase];

  return (
    <div style={{ minHeight: "100vh", background: "radial-gradient(1000px 500px at 75% -5%, #16233c, " + INK + " 60%)", color: "#E7ECF6", fontFamily: "'IBM Plex Sans', system-ui, sans-serif", padding: "clamp(18px,3.5vw,44px)" }}>
      <style>{"@import url('https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600&family=IBM+Plex+Mono:wght@500&family=Fraunces:opsz,wght@9..144,500&display=swap');*{box-sizing:border-box}.mono{font-family:'IBM Plex Mono',monospace}.disp{font-family:'Fraunces',serif}"}</style>

      <div style={{ maxWidth: 1080, margin: "0 auto" }}>
        <div className="mono" style={{ color: TEAL, fontSize: 11, letterSpacing: 3, marginBottom: 8 }}>BLUEMETA PULSE · SYSTEM DESIGN + BUILD TRACKER</div>
        <h1 className="disp" style={{ fontSize: "clamp(26px,4.5vw,44px)", margin: "0 0 6px" }}>Helicopter View</h1>
        <p style={{ color: MUTE, maxWidth: 620, margin: "0 0 22px", lineHeight: 1.55, fontSize: 14.5 }}>
          Two zones, Go + ADK on Vertex AI. Step through the phase tracker to see which nodes come online and what infrastructure deploys at each phase. Tap any node for detail.
        </p>

        <div style={{ background: PANEL, border: "1px solid " + LINE, borderRadius: 14, padding: "14px 16px", marginBottom: 18 }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12, flexWrap: "wrap", gap: 8 }}>
            <span className="mono" style={{ fontSize: 11, letterSpacing: 1.5, color: MUTE }}>PHASE TRACKER</span>
            <button onClick={() => setCumulative(!cumulative)} style={{ cursor: "pointer", background: cumulative ? TEAL : PANEL2, color: cumulative ? INK : MUTE, border: "1px solid " + (cumulative ? TEAL : LINE), borderRadius: 20, padding: "4px 12px", fontSize: 11.5, fontWeight: 600 }}>
              {cumulative ? "Cumulative \u2713" : "Single phase"}
            </button>
          </div>
          <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
            {PHASES.map((p, i) => (
              <button key={p.n} onClick={() => setPhaseIdx(i)} style={{ cursor: "pointer", flex: "1 1 90px", background: i === phaseIdx ? p.accent : PANEL2, color: i === phaseIdx ? INK : "#C2CCDC", border: "1px solid " + (i === phaseIdx ? p.accent : LINE), borderRadius: 9, padding: "8px 6px", transition: "all .15s", textAlign: "left" }}>
                <div className="mono" style={{ fontSize: 10.5, opacity: 0.8 }}>PHASE {p.n}</div>
                <div style={{ fontSize: 12, fontWeight: 600, marginTop: 2 }}>{p.name}</div>
              </button>
            ))}
          </div>
        </div>

        <div style={{ display: "grid", gridTemplateColumns: "1fr", gap: 16 }}>
          <div style={{ background: PANEL, border: "1px solid " + LINE, borderLeft: "3px solid " + TEAL, borderRadius: 14, padding: "16px 18px" }}>
            <div className="mono" style={{ color: TEAL, fontSize: 11, letterSpacing: 1.5, marginBottom: 12 }}>ZONE 1 - SCHEDULED PIPELINE · NO MANAGER · RUNS DAILY</div>
            <div style={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 4 }}>
              <Node id="hunter" color={BLUE} title="Hunter" sub="Pulls + filters SAM.gov" w={190} active={sel === "hunter"} dimmed={!isLive("hunter")} onClick={setSel} />
              <Arrow />
              <Node id="scorer" color={BLUE} title="Scorer" sub="Bid/no-bid + reasoning" w={190} active={sel === "scorer"} dimmed={!isLive("scorer")} onClick={setSel} />
              <Arrow />
              <Node id="queue" color={TEAL} title="Opportunity Queue" sub="Ranked dashboard" w={190} active={sel === "queue"} dimmed={!isLive("queue")} onClick={setSel} />
            </div>
          </div>

          <div style={{ display: "flex", justifyContent: "center" }}>
            <div onClick={() => setSel("trigger")} style={{ cursor: "pointer", background: sel === "trigger" ? AMBER : PANEL2, border: "1px dashed " + AMBER, borderRadius: 30, padding: "8px 20px", fontSize: 12.5, fontWeight: 600, color: sel === "trigger" ? INK : AMBER, opacity: isLive("trigger") ? 1 : 0.32 }}>
              \u2193 Selection event spins up a Manager \u2193
            </div>
          </div>

          <div style={{ background: PANEL, border: "1px solid " + LINE, borderLeft: "3px solid " + PURPLE, borderRadius: 14, padding: "16px 18px", position: "relative" }}>
            <div style={{ position: "absolute", top: -6, left: 10, right: 10, height: 14, background: PANEL2, border: "1px solid " + LINE, borderRadius: 14, opacity: 0.5 }} />
            <div style={{ position: "absolute", top: -3, left: 6, right: 6, height: 14, background: PANEL2, border: "1px solid " + LINE, borderRadius: 14, opacity: 0.75 }} />
            <div style={{ position: "relative" }}>
              <div className="mono" style={{ color: PURPLE, fontSize: 11, letterSpacing: 1.5, marginBottom: 4 }}>ZONE 2 - PER-PROPOSAL LIFECYCLE · MANAGER-ORCHESTRATED</div>
              <div style={{ fontSize: 11, color: MUTE, marginBottom: 12 }}>One stack per proposal - many run concurrently at scale (layered edges above).</div>
              <Node id="manager" color={AMBER} title="Manager Agent" sub="Coordinates · pauses at gate · resumes" w="100%" active={sel === "manager"} dimmed={!isLive("manager")} onClick={setSel} />
              <Arrow vertical />
              <div style={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 4 }}>
                <Node id="outline" color={BLUE} title="Outline" sub="Proposal skeleton" w={150} active={sel === "outline"} dimmed={!isLive("outline")} onClick={setSel} />
                <Arrow />
                <Node id="writer" color={BLUE} title="Technical Writer" sub="Fills from KB · flags gaps" w={180} active={sel === "writer"} dimmed={!isLive("writer")} onClick={setSel} />
                <Arrow />
                <Node id="gate" color={RED} title="Human Gate" sub="Review · approve" w={150} active={sel === "gate"} dimmed={!isLive("gate")} onClick={setSel} />
                <Arrow />
                <Node id="final" color={BLUE} title="Final Review" sub="QA + compliance" w={150} active={sel === "final"} dimmed={!isLive("final")} onClick={setSel} />
              </div>
            </div>
          </div>
        </div>

        <div style={{ marginTop: 18, background: PANEL, border: "1px solid " + phase.accent + "55", borderLeft: "3px solid " + phase.accent, borderRadius: 14, padding: "18px 20px" }}>
          <div style={{ display: "flex", alignItems: "baseline", gap: 12, flexWrap: "wrap", marginBottom: 4 }}>
            <span style={{ fontSize: 16, fontWeight: 600 }}>{cumulative ? "Infrastructure through Phase " + phase.n : "Phase " + phase.n + " - " + phase.name}</span>
            <span className="mono" style={{ fontSize: 11, color: phase.accent }}>{cumulative ? "CUMULATIVE" : "THIS PHASE ONLY"}</span>
          </div>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16, marginTop: 12 }}>
            <div>
              <div className="mono" style={{ fontSize: 10.5, letterSpacing: 1, color: MUTE, marginBottom: 8 }}>GCP SERVICES TO DEPLOY</div>
              {shownPhases.map((p) => p.gcp.map((g) => (
                <div key={p.n + g} style={{ fontSize: 13, color: "#D4DCEA", padding: "5px 0", borderBottom: "1px solid " + LINE, display: "flex", gap: 8 }}>
                  <span className="mono" style={{ color: p.accent, fontSize: 10.5, minWidth: 14 }}>P{p.n}</span>{g}
                </div>
              )))}
            </div>
            <div>
              <div className="mono" style={{ fontSize: 10.5, letterSpacing: 1, color: MUTE, marginBottom: 8 }}>INTERFACES / DATA LAYERS</div>
              {shownPhases.map((p) => p.data.map((g) => (
                <div key={p.n + g} style={{ fontSize: 13, color: "#D4DCEA", padding: "5px 0", borderBottom: "1px solid " + LINE, display: "flex", gap: 8 }}>
                  <span className="mono" style={{ color: p.accent, fontSize: 10.5, minWidth: 14 }}>P{p.n}</span>{g}
                </div>
              )))}
            </div>
          </div>
          <div style={{ marginTop: 14, fontSize: 12.5, color: phase.accent, background: INK, border: "1px solid " + phase.accent + "33", borderRadius: 8, padding: "9px 12px" }}>
            \u25C6 {phase.note}
          </div>
        </div>

        <div style={{ marginTop: 14, background: PANEL, border: "1px solid " + d.color + "55", borderLeft: "3px solid " + d.color, borderRadius: 14, padding: "16px 18px" }}>
          <div style={{ display: "flex", alignItems: "baseline", gap: 12, flexWrap: "wrap" }}>
            <span style={{ fontSize: 15.5, fontWeight: 600 }}>{d.title}</span>
            <span className="mono" style={{ fontSize: 11, color: d.color, letterSpacing: 1 }}>{d.zone.toUpperCase()}</span>
          </div>
          <p style={{ color: "#C2CCDC", lineHeight: 1.55, margin: "8px 0 10px", fontSize: 14 }}>{d.body}</p>
          <div className="mono" style={{ fontSize: 12, color: MUTE, background: INK, border: "1px solid " + LINE, borderRadius: 8, padding: "8px 11px" }}>{d.io}</div>
        </div>

        <div style={{ display: "flex", gap: 18, marginTop: 18, flexWrap: "wrap", color: "#71809c", fontSize: 12 }}>
          <span><span style={{ color: BLUE }}>\u25CF</span> Agent</span>
          <span><span style={{ color: TEAL }}>\u25CF</span> Shared store</span>
          <span><span style={{ color: AMBER }}>\u25CF</span> Orchestration / trigger</span>
          <span><span style={{ color: RED }}>\u25CF</span> Human gate</span>
          <span style={{ marginLeft: "auto", opacity: 0.7 }}>dimmed = not yet built at this phase</span>
        </div>
      </div>
    </div>
  );
}

export default App;
