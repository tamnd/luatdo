package graph

// Style is the Neo4j Browser grass file the export ships beside the data.
//
// A graph browser with no style file shows every node as a grey circle captioned
// with an internal identifier, and the first thing anyone does with a new dump is
// spend twenty minutes clicking nodes to work out which is which. The caption on
// each label here is the property a Vietnamese lawyer would use to name the thing
// out loud: a document is its official number, a component is its number, a
// concept is its Vietnamese label. Nothing is captioned with an id.
//
// The colours group by layer rather than by taste. Documents and components are
// blue, the definition side is green, norms and their satellites are orange, the
// concept layer is purple, the temporal layer is red, and the act layer is
// amber, so a screenshot of a query result says which layers it crossed before
// anyone reads a word of it.
//
// Component and Provision get identical rules because they are the same nodes
// under two labels, and the Browser styles a node by whichever label it was
// shown under. When the alias is dropped, its rule goes with it.
//
// The file has no comments in it. The grass parser has no comment syntax, so
// anything worth saying about the style is said here.
//
// To use it: open the Browser, drop the file on the window, or run
// :style and paste the contents.
const Style = `node {
  diameter: 50px;
  color: #A5ABB6;
  border-color: #9AA1AC;
  border-width: 2px;
  text-color-internal: #FFFFFF;
  font-size: 10px;
}

relationship {
  color: #A5ABB6;
  shaft-width: 1px;
  font-size: 8px;
  padding: 3px;
  text-color-external: #000000;
  text-color-internal: #FFFFFF;
  caption: '<type>';
}

node.Document {
  color: #4C8EDA;
  border-color: #2870C2;
  text-color-internal: #FFFFFF;
  diameter: 80px;
  caption: '{official_number}';
}

node.Component {
  color: #A5ABB6;
  border-color: #9AA1AC;
  diameter: 65px;
  caption: '{number}';
}

node.Provision {
  color: #A5ABB6;
  border-color: #9AA1AC;
  diameter: 65px;
  caption: '{number}';
}

node.TextVersion {
  color: #C0C6CC;
  border-color: #A5ABB6;
  text-color-internal: #2A2C34;
  diameter: 50px;
  caption: '{from_date}';
}

node.Term {
  color: #57C7E3;
  border-color: #23B3D7;
  text-color-internal: #2A2C34;
  caption: '{text}';
}

node.LegalConcept {
  color: #8DCC93;
  border-color: #5DB665;
  text-color-internal: #2A2C34;
  diameter: 65px;
  caption: '{label_vi}';
}

node.Subject {
  color: #569480;
  border-color: #447666;
  caption: '{label_vi}';
}

node.Norm {
  color: #F79767;
  border-color: #F36924;
  diameter: 65px;
  caption: '{action}';
}

node.Condition {
  color: #FFC454;
  border-color: #D7A013;
  text-color-internal: #2A2C34;
  caption: '{text}';
}

node.Exception {
  color: #FFC454;
  border-color: #D7A013;
  text-color-internal: #2A2C34;
  caption: '{text}';
}

node.Sanction {
  color: #DA7194;
  border-color: #CC3C6C;
  caption: '{text}';
}

node.TermUse {
  color: #ECB5C9;
  border-color: #DA7298;
  text-color-internal: #2A2C34;
  diameter: 65px;
  caption: '{label_vi}';
}

node.Concept {
  color: #C990C0;
  border-color: #B261A5;
  diameter: 80px;
  caption: '{label_vi}';
}

node.Event {
  color: #D9534F;
  border-color: #C9302C;
  diameter: 65px;
  caption: '{kind}';
}

node.TemporalVersion {
  color: #F2A0A0;
  border-color: #D9534F;
  text-color-internal: #2A2C34;
  diameter: 65px;
  caption: '{from_date}';
}

node.Conflict {
  color: #C9302C;
  border-color: #8B1A1A;
  text-color-internal: #FFFFFF;
  diameter: 65px;
  caption: '{rule}';
}

node.Act {
  color: #FFC454;
  border-color: #D7A013;
  text-color-internal: #2A2C34;
  diameter: 65px;
  caption: '{label_vi}';
}

relationship.CONTAINS { color: #A5ABB6; shaft-width: 1px; }
relationship.CITES { color: #4C8EDA; shaft-width: 2px; }
relationship.AMENDS { color: #D9534F; shaft-width: 3px; }
relationship.DEFINES { color: #8DCC93; shaft-width: 2px; }
relationship.DEFINES_TERM { color: #8DCC93; shaft-width: 2px; }
relationship.MENTIONS { color: #C0C6CC; shaft-width: 1px; }
relationship.HAS_NORM { color: #F79767; shaft-width: 2px; }
relationship.HAS_BEARER { color: #F36924; shaft-width: 2px; }
relationship.HAS_COUNTERPARTY { color: #F79767; shaft-width: 1px; }
relationship.HAS_SANCTION { color: #CC3C6C; shaft-width: 2px; }
relationship.INSTANCE_OF { color: #C990C0; shaft-width: 2px; }
relationship.DIFFERS_FROM { color: #B261A5; shaft-width: 3px; }
relationship.ABOUT_CONCEPT { color: #C990C0; shaft-width: 2px; caption: '{role}'; }
relationship.CONCEPT_BROADER { color: #B261A5; shaft-width: 2px; }
relationship.REQUIRES { color: #B261A5; shaft-width: 2px; }
relationship.HAS_TEMPORAL_VERSION { color: #F2A0A0; shaft-width: 2px; }
relationship.PRODUCES_VERSION { color: #C9302C; shaft-width: 2px; }
relationship.TERMINATES { color: #C9302C; shaft-width: 2px; }
relationship.CAUSED_BY { color: #D9534F; shaft-width: 2px; }
relationship.INVOLVES { color: #C9302C; shaft-width: 2px; caption: '{side}'; }
relationship.TRIGGERS { color: #D7A013; shaft-width: 3px; }
relationship.PRECEDES { color: #FFC454; shaft-width: 2px; }
relationship.PRECONDITION_OF { color: #FFC454; shaft-width: 2px; }
relationship.PRECLUDES { color: #D7A013; shaft-width: 2px; }
relationship.HAS_PARTICIPANT { color: #FFD86E; shaft-width: 1px; caption: '{role}'; }
relationship.ABOUT_ACT { color: #F79767; shaft-width: 2px; caption: '{slot}'; }
`
