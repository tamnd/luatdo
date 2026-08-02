// Question 12. Every deadline shorter than a given number of working days, with
// the actor who must meet it.
//
// The calendar filter is the whole query. "5 ngày" and "5 ngày làm việc" are five
// calendar days and five working days, which can be nine days apart over Tet, and
// a query that counted both would report deadlines that are not short and miss
// the argument for asking. The anchor is returned because a deadline nobody can
// date is a deadline nobody can meet.
MATCH (p:Component)-[:HAS_NORM]->(n:Norm)
WHERE n.deadline_value > 0
  AND n.deadline_value < $days
  AND n.deadline_unit = 'day'
  AND n.deadline_calendar = 'working'
RETURN n.bearer AS actor,
       n.action AS action,
       n.deadline_text AS deadline,
       n.deadline_value AS working_days,
       n.deadline_anchor AS counted_from,
       p.id AS provision
ORDER BY working_days, provision
LIMIT $limit
