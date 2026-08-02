// Question 9. What duties does one instrument place on a given class of actor,
// and which of them carry a sanction?
//
// The bearer edge is HAS_BEARER and not HAS_COUNTERPARTY on purpose. "Bên A phải
// thông báo cho bên B" names two actors and only one of them owes the duty, and a
// query that accepts either edge answers this question with the wrong half of the
// corpus. Rows with no sanction come last rather than being dropped: a duty with
// no consequence attached is one of the more interesting answers here.
MATCH (d:Document {id: $doc})-[:CONTAINS*1..8]->(p:Component)-[:HAS_NORM]->(n:Norm)
WHERE n.norm_type = $norm_type
MATCH (n)-[:HAS_BEARER]->(:LegalConcept {id: $class})
OPTIONAL MATCH (n)-[:HAS_SANCTION]->(s:Sanction)
RETURN p.id AS provision,
       n.action AS action,
       n.object AS object,
       n.deadline_text AS deadline,
       s.text AS sanction,
       s.legal_basis AS sanction_basis,
       s IS NOT NULL AS carries_sanction
ORDER BY carries_sanction DESC, provision
LIMIT $limit
