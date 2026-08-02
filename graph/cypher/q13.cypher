// Question 13. Which prohibitions have no corresponding sanction anywhere in the
// corpus?
//
// Anywhere is the hard part. A prohibition in a law is very often sanctioned by a
// decree on administrative penalties that never cites the law back, so a check
// confined to the prohibition's own document would report almost every one of them
// as unsanctioned. The second condition walks out through the concept the norm is
// about and looks for any norm at all that carries a sanction, which is as close
// to anywhere as the graph can get without reading the text again.
MATCH (p:Component)-[:HAS_NORM]->(n:Norm)
WHERE n.norm_type = 'prohibition'
  AND NOT EXISTS { MATCH (n)-[:HAS_SANCTION]->() }
  AND NOT EXISTS {
    MATCH (n)-[:ABOUT_CONCEPT]->(:Concept)<-[:ABOUT_CONCEPT]-(other:Norm)-[:HAS_SANCTION]->()
    WHERE other <> n
  }
RETURN p.id AS provision,
       n.bearer AS bearer,
       n.action AS action,
       n.object AS object,
       n.evidence_quote AS quote
ORDER BY provision
LIMIT $limit
