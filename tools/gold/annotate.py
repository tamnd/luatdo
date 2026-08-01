#!/usr/bin/env python3
"""Turn the drawn gold candidates into the annotated gold set.

The annotations below were written by reading the two hundred clauses drawn by
"luatdo concepts sample", before any model was pointed at them. The table is
keyed by the identifier of the clause. It was keyed by position at first, which
was wrong: a fix in the anchor stage changed four of the clauses the seed draws
and every annotation after the first of them would have quietly described the
clause above it. An identifier cannot slide, and a redraw that moves a clause in
or out now fails loudly with its identifier in the message.

Each value is a list of terms. An empty list means the clause defines nothing,
which is an answer and not a gap. A term is
(label, genus, kind, is_role, by_reference, [subtypes], [aliases])
with everything after the kind optional.

Run it from the repository root. It checks that every genus and every subtype it
was given is really present in the clause, because an annotation that quotes
text the clause does not contain is an annotation of something else.
"""

import json
import os
import sys
import unicodedata

ACTOR, BODY, ACTION, ARTIFACT = "actor", "body", "action", "artifact"
THING, PLACE, TIME, AMOUNT = "thing", "place", "time", "amount"
STATUS, CONDITION, RULE, OTHER = "status", "condition", "rule", "other"


def t(label, genus, kind, role=False, ref="", subtypes=None, aliases=None):
    return {
        "label_vi": label,
        "genus": genus,
        "kind": kind,
        "is_role": role,
        "defines_by_reference": ref,
        "enumerated_subtypes": subtypes or [],
        "aliases": aliases or [],
    }


NOTHING = []

ANNOTATIONS = {
    "vn:law:2019:45-2019-qh14:article-3:clause-7":
        [t("Cưỡng bức lao động", "việc dùng vũ lực", ACTION)],
    "vn:law:2012:11-2012-qh13:article-4:clause-13":
        [t("Mặt bằng giá", "mức trung bình của các mức giá", AMOUNT)],
    "vn:law:2006:79-2006-qh11:article-3:clause-20":
        [t("Lòng sông", "phạm vi giữa hai bờ sông", PLACE)],
    "vn:law:2015:83-2015-qh13:article-4:clause-10":
        [t("Đơn vị dự toán ngân sách", "cơ quan, tổ chức, đơn vị", ACTOR)],
    "vn:law:2023:28-2023-qh15:article-2:clause-1":
        [t("Tài nguyên nước", "", THING, subtypes=[
        "nước mặt", "nước dưới đất", "nước mưa", "nước biển"])],
    "vn:law:2025:71-2025-qh15:article-3:clause-9":
        [t("Hệ thống trí tuệ nhân tạo", "hệ thống", THING)],
    "vn:law:2005:60-2005-qh11:article-4:clause-10":
        [t("Thành viên sáng lập", "người góp vốn", ACTOR)],
    "vn:law:2025:93-2025-qh15:article-3:clause-1":
        [t("Khoa học", "hệ thống tri thức", OTHER)],
    "vn:law:2022:12-2022-qh15:article-3:clause-37":
        [t("Trữ lượng dầu khí", "lượng dầu khí", AMOUNT)],
    "vn:law:2025:135-2025-qh15:article-3:clause-6":
        [t("Dự án đầu tư xây dựng", "tập hợp các đề xuất", OTHER)],
    "vn:law:2000:22-2000-qh10:article-8:clause-11":
        [t("Cấp dưỡng", "việc một người có nghĩa vụ đóng góp tiền hoặc tài sản khác", ACTION)],
    "vn:law:2025:80-2025-qh15:article-5:clause-8":
        [t("Bổ nhiệm", "việc cơ quan, tổ chức có thẩm quyền quyết định giao cán bộ, công chức giữ một chức vụ", ACTION)],
    "vn:law:2019:39-2019-qh14:article-4:clause-3":
        [t("Báo cáo nghiên cứu khả thi", "tài liệu", ARTIFACT)],
    "vn:law:2000:22-2000-qh10:article-8:clause-4":
        [t("Tảo hôn", "việc lấy vợ, lấy chồng", ACTION)],
    "vn:law:2019:41-2019-qh14:article-3:clause-15":
        [t("Thi hành biện pháp tư pháp giáo dục tại trường giáo dưỡng", "việc", ACTION)],
    "vn:law:2005:35-2005-qh11:article-3:clause-1":
        [t("Bao gửi", "hàng hoá", THING)],
    "vn:law:2013:35-2013-qh13:article-2:clause-1":
        [t("Hòa giải ở cơ sở", "việc hòa giải viên hướng dẫn, giúp đỡ các bên", ACTION)],
    "vn:law:2014:50-2014-qh13:article-3:clause-42":
        [t("Thiết kế kỹ thuật", "thiết kế", ARTIFACT)],
    "vn:law:2020:130-2020-qh14:article-3:clause-6":
        [t("Cử luân phiên, thay thế", "việc cử lực lượng", ACTION)],
    "vn:law:2025:29-2025-nq-hdnd:hdnd-thanh-pho-ha-noi:article-3:clause-1":
        [t("Sản phẩm thử nghiệm có kiểm soát", "sản phẩm, công nghệ, dịch vụ", THING)],
    "vn:law:2026:67-2026-nq-hdnd:hdnd-thanh-pho-ha-noi:article-3:clause-4":
        [t("Hệ số lợi thế TOD", "hệ số", AMOUNT)],
    "vn:law:2024:28-2024-nq-hdnd:hdnd-tinh-an-giang:article-2":
        [t("Khu dân cư", "", PLACE, subtypes=[
        "khu di tích lịch sử - văn hóa", "khu danh lam thắng cảnh", "khu du lịch",
        "cơ sở tôn giáo", "trường học", "bệnh viện", "trung tâm y tế", "chợ",
        "trụ sở làm việc của cơ quan nhà nước", "khu công nghiệp", "cụm công nghiệp"])],
    "vn:law:2007:121-2007-nd-cp:article-3:clause-1":
        [t("Hoạt động dầu khí", "hoạt động", ACTION)],
    "vn:law:2023:67-2023-nd-cp:article-3:clause-2":
        [t("Xe cơ giới hoạt động", "xe cơ giới", THING)],
    "vn:law:2008:55-2008-nd-cp:article-3:clause-1":
        [t("Tổ chức, cá nhân kinh doanh hàng hóa, dịch vụ", "tổ chức, cá nhân", ACTOR, role=True)],
    "vn:law:2013:08-2013-nd-cp:article-3:clause-5":
        [t("Phương tiện vi phạm", "", THING, subtypes=[
        "phương tiện vận tải", "công cụ", "máy móc"])],
    "vn:law:2012:58-2012-nd-cp:article-2:clause-18":
        [t("Ngân hàng giám sát", "ngân hàng thương mại", ACTOR, role=True)],
    "vn:law:2026:243-2026-nd-cp:article-2:clause-5":
        [t("Sản lượng điện dư", "sản lượng điện", AMOUNT)],
    "vn:law:2024:68-2024-nd-cp:article-3:clause-15":
        [t("PKI Token", "thiết bị", THING)],
    "vn:law:2009:105-2009-nd-cp:article-3:clause-3":
        [t("Chiếm đất", "việc sử dụng đất", ACTION)],
    "vn:law:2025:332-2025-nd-cp:article-2:clause-12":
        [t("Lưu giữ nguồn phóng xạ", "lưu giữ nguồn phóng xạ", ACTION)],
    "vn:law:2019:81-2019-nd-cp:article-4:clause-2":
        [t("Vũ khí hạt nhân", "vũ khí", THING)],
    "vn:law:2020:11-2020-nd-cp:article-3:clause-6":
        [t("Các đơn vị, tổ chức thuộc đối tượng mở tài khoản tại Kho bạc Nhà nước",
           "các đơn vị sử dụng ngân sách nhà nước", ACTOR, role=True)],
    "vn:law:2017:05-2017-nd-cp:article-3:clause-4":
        [t("Trục vớt tài sản chìm đắm", "các hoạt động", ACTION, subtypes=[
        "Thăm dò", "di dời", "phá dỡ", "phá hủy tài sản chìm đắm"])],
    "vn:law:2025:57-2025-nd-cp:article-3:clause-20":
        [t("Tổng công ty Điện lực", "", BODY, subtypes=[
        "Tổng công ty Điện lực miền Bắc", "Tổng công ty Điện lực miền Nam",
        "Tổng công ty Điện lực miền Trung", "Tổng công ty Điện lực thành phố Hà Nội",
        "Tổng công ty Điện lực Thành phố Hồ Chí Minh"])],
    "vn:law:1998:48-1998-nd-cp:article-2:clause-12":
        [t("Quản lý danh mục đầu tư", "hoạt động quản lý vốn", ACTION)],
    "vn:law:2015:78-2015-nd-cp:article-3:clause-9":
        [t("Tài khoản đăng ký kinh doanh", "tài khoản", ARTIFACT)],
    "vn:law:2005:04-2005-nd-cp:article-4:clause-12":
        [t("Chánh thanh tra Sở", "Chánh thanh tra Sở", BODY)],
    "vn:law:2014:38-2014-nd-cp:article-4:clause-4":
        [t("Chất chống bạo loạn", "hóa chất", THING)],
    "vn:law:2026:112-2026-nd-cp:article-3:clause-9":
        [t("Cơ quan giám sát Cơ chế Điều 6.4", "cơ quan", BODY)],
    "vn:law:2012:58-2012-nd-cp:article-2:clause-15":
        NOTHING,
    "vn:law:2009:106-2009-nd-cp:article-3:clause-2":
        [t("Tài sản chuyên dùng", "tài sản", THING)],
    "vn:law:2015:111-2015-nd-cp:article-3:clause-2":
        [t("Dự án sản xuất công nghiệp hỗ trợ", "dự án", OTHER)],
    "vn:law:2009:84-2009-nd-cp:article-3:clause-7":
        [t("Giá xăng dầu thế giới", "giá", AMOUNT)],
    "vn:law:2013:52-2013-nd-cp:article-3:clause-2":
        [t("Chương trình phát triển thương mại điện tử quốc gia", "tập hợp các nội dung", OTHER)],
    "vn:law:2026:112-2026-nd-cp:article-3:clause-10":
        [t("Hệ thống đăng ký của Cơ chế Điều 6.4", "hệ thống", THING)],
    "vn:law:2024:69-2024-nd-cp:article-3:clause-8":
        [t("Phương tiện xác thực", "một số phương pháp", THING, subtypes=[
        "mật khẩu", "mã bí mật", "mã vạch", "thiết bị đầu cuối", "thẻ căn cước",
        "hộ chiếu", "ảnh khuôn mặt", "vân tay", "giọng nói", "mống mắt"])],
    "vn:law:2024:42-2024-nd-cp:article-3:clause-1":
        [t("Lấn biển", "việc mở rộng diện tích đất", ACTION)],
    "vn:law:2025:72-2025-nd-cp:article-2:clause-2":
        [t("Bên bán điện", "đơn vị phát điện, tổ chức, cá nhân", ACTOR, role=True)],
    "vn:law:2006:105-2006-nd-cp:article-3:clause-2":
        [t("Xử lý hành vi xâm phạm", "xử lý hành vi xâm phạm quyền sở hữu trí tuệ", ACTION)],
    "vn:law:2013:72-2013-nd-cp:article-3:clause-17":
        [t("Dịch vụ nội dung thông tin", "dịch vụ", ACTION)],
    "vn:law:2015:59-2015-nd-cp:article-2:clause-12":
        [t("Tổng thầu xây dựng thực hiện hợp đồng chìa khóa trao tay", "nhà thầu", ACTOR, role=True)],
    "vn:law:2018:24-2018-nd-cp:article-3:clause-19":
        [t("Hành vi về giáo dục nghề nghiệp",
            "hành vi của người đứng đầu cơ sở giáo dục nghề nghiệp", ACTION)],
    "vn:law:1998:07-1998-pl-ubtvqh10:article-2:clause-8":
        [t("Đình chỉ hiệu lực", "tuyên bố", ACTION)],
    "vn:law:1999:17-1999-pl-ubtvqh10:article-3:clause-5":
        [t("Người bị ký phát", "người", ACTOR, role=True)],
    "vn:law:2017:26-2017-qd-ubnd:ubnd-tinh-bac-kan:annex-1:article-4:clause-7":
        [t("BĐKH", "sự thay đổi của khí hậu", OTHER)],
    "vn:law:2015:73-2015-qd-ubnd:ubnd-tinh-ninh-thuan:annex-1:article-3:clause-11":
        [t("Cuộc họp của UBND tỉnh, huyện, thành phố, xã, phường, thị trấn", "cuộc họp", ACTION)],
    "vn:law:2016:54-2016-qd-ubnd:ubnd-tinh-dong-thap:annex-1:article-3:clause-5":
        NOTHING,
    "vn:law:2022:37-2022-qd-ubnd:ubnd-tinh-ninh-thuan:article-2:clause-6":
        [t("Đơn vị vật nuôi", "đơn vị quy đổi", AMOUNT, aliases=["ĐVN"])],
    "vn:law:2010:28-2010-qd-ubnd:ubnd-tinh-dak-nong:annex-1:article-3:clause-5":
        [t("TCVN 7562:2005", "Tiêu chuẩn Việt Nam", RULE)],
    "vn:law:2014:73-2014-qd-ubnd:ubnd-tinh-dong-nai:annex-1:article-2:clause-1":
        [t("Tổ chức PCPNN", "", ACTOR, ref="Quy định này")],
    "vn:law:2010:81-2010-qd-ttg:article-3:clause-11":
        [t("Lưu trữ dữ liệu viễn thám", "quá trình", ACTION)],
    "vn:law:2016:10-2016-qd-ttg:article-3:clause-7":
        [t("Xác nhận hoàn thành thủ tục biên phòng điện tử", "việc", ACTION)],
    "vn:law:2008:03-2008-qd-ubnd:ubnd-tinh-bac-lieu:annex-1:article-2:clause-6":
        [t("Khu sản xuất, kinh doanh, dịch vụ tập trung", "", PLACE, subtypes=[
        "Khu kinh tế", "khu công nghiệp", "khu chế xuất", "khu công nghệ cao",
        "cụm công nghiệp", "khu du lịch", "khu vui chơi giải trí tập trung"])],
    "vn:law:2007:77-2007-qd-ubnd:ubnd-tinh-binh-thuan:annex-1:article-2:clause-5":
        [t("Người có thẩm quyền", "người được quyền quyết định", ACTOR, role=True)],
    "vn:law:2008:10-2008-qd-ubnd:ubnd-tinh-lam-dong:annex-1:article-2:clause-6":
        [t("Sản phẩm hàng mộc", "sản phẩm", THING, subtypes=[
        "bàn", "ghế", "giường", "tủ", "khay", "kệ", "trục mành", "hộp đựng dao"])],
    "vn:law:2016:4715-2016-qd-ubnd:ubnd-tinh-thanh-hoa:annex-1:article-2:clause-2":
        [t("Cơ quan quản lý kinh phí nhiệm vụ", "Sở Tài chính", BODY)],
    "vn:law:2004:45-2004-qd-bnn:article-3:clause-7":
        [t("Chủ quản đầu tư", "Bộ Nông nghiệp và Phát triển nông thôn", BODY,
            aliases=["cơ quan chủ quản chương trình, dự án"])],
    "vn:law:2024:29-2024-qd-ubnd:ubnd-tinh-tien-giang:annex-1:article-3:clause-21":
        [t("Tầng lửng", "tầng", PLACE)],
    "vn:law:2016:10-2016-qd-ubnd:ubnd-tinh-long-an:annex-1:article-3:clause-1":
        [t("Nguồn lợi thủy sản", "tài nguyên sinh vật", THING)],
    "vn:law:2024:07-2024-qd-ubnd:ubnd-tinh-ha-nam:annex-1:article-3:clause-2~4": NOTHING,
    "vn:law:2010:97-2010-qd-ubnd:ubnd-tinh-bac-giang:annex-1:article-3:clause-3":
        [t("Hệ thống công trình thủy lợi liên tỉnh", "hệ thống công trình", THING)],
    "vn:law:2013:1046-2013-qd-ubnd:ubnd-tinh-cao-bang:annex-1:article-2:clause-2":
        [t("Bản gốc văn bản", "bản", ARTIFACT)],
    "vn:law:2016:23-2016-qd-ubnd:ubnd-tinh-binh-duong:annex-1:article-3:clause-19":
        [t("Giấy phép xử lý chất thải nguy hại", "giấy phép", ARTIFACT)],
    "vn:law:2010:35-2010-qd-ubnd:ubnd-thanh-pho-ha-noi:annex-1:article-3:clause-19":
        [t("Vùng hạn chế khai thác, vùng cấm khai thác", "vùng địa lý", PLACE)],
    "vn:law:2026:38-2026-qd-ubnd:ubnd-tinh-dak-lak:article-3:clause-2":
        [t("Bảng giá đất", "Bảng giá đất", ARTIFACT)],
    "vn:law:2012:02-2012-qd-ubnd:ubnd-tinh-quang-ngai:annex-1:article-4:clause-1":
        [t("Phí chợ", "khoản thu", AMOUNT)],
    "vn:law:2018:10-2018-qd-ubnd:ubnd-tinh-quang-binh:annex-1:article-3:clause-5":
        [t("Chủ sở hữu cột treo cáp", "đơn vị", ACTOR, role=True)],
    "vn:law:2017:50-2017-qd-ubnd:ubnd-tinh-an-giang:annex-1:article-3:clause-1":
        [t("Xe buýt", "xe", THING)],
    "vn:law:2026:74-2026-qd-ubnd:ubnd-tinh-gia-lai:annex-1:article-3:clause-2":
        [t("Chủ quản lý công trình thủy lợi", "cơ quan", BODY, role=True)],
    "vn:law:1999:4196-1999-qd-byt:annex-1:article-3:clause-12":
        [t("Tiêu chuẩn thực phẩm", "văn bản kỹ thuật", ARTIFACT)],
    "vn:law:2010:21-2010-qd-ubnd:ubnd-tinh-yen-bai:annex-1:article-2:clause-17":
        [t("Quan trắc môi trường", "quá trình theo dõi", ACTION)],
    "vn:law:2013:1016-2013-qd-ubnd:ubnd-tinh-bac-kan:annex-1:article-3:clause-4":
        [t("Ống đấu nối", "đường ống", THING)],
    "vn:law:2017:60-2017-qd-ubnd:ubnd-tinh-phu-yen:annex-1:article-2:clause-2":
        [t("Độ vươn", "khoảng cách", AMOUNT)],
    "vn:law:2007:08-2007-qd-ubnd:ubnd-quan-4:annex-1:article-2:clause-1":
        [t("Ngày làm việc", "tổng số ngày trong tuần", TIME)],
    "vn:law:2015:21-2015-qd-ubnd:ubnd-tinh-hung-yen:annex-1:article-3:clause-1":
        [t("Nhiệm vụ do Chính phủ, Thủ tướng Chính phủ giao", "những nhiệm vụ", ACTION)],
    "vn:law:2015:28-2015-qd-ubnd:ubnd-tinh-phu-yen:annex-1:article-2:clause-2":
        [t("Điểm đấu nối", "các điểm xả nước", PLACE)],
    "vn:law:2012:27-2012-qd-ubnd:ubnd-tinh-quang-binh:annex-1:article-2:clause-1":
        [t("Cụm hay khu dân cư tập trung", "một khu vực", PLACE)],
    "vn:law:2025:16-2025-qd-ubnd:ubnd-tinh-cao-bang:annex-1:article-2:clause-2":
        [t("Hệ thống thư điện tử công vụ", "hệ thống thông tin", THING)],
    "vn:law:2010:44-2010-qd-ubnd:hdnd-thanh-pho-ha-noi:annex-1:article-2:clause-8":
        [t("Tỷ lệ lấp đầy", "tỷ lệ", AMOUNT)],
    "vn:law:2016:34-2016-qd-ubnd:ubnd-tinh-yen-bai:annex-1:article-4:clause-5":
        [t("Cơ quan nhà nước có thẩm quyền", "cơ quan", BODY, role=True)],
    "vn:law:2007:77-2007-qd-ubnd:ubnd-tinh-binh-thuan:annex-1:article-2:clause-7":
        [t("Chủ đầu tư", "người sở hữu vốn", ACTOR, role=True)],
    "vn:law:2016:38-2016-qd-ttg:article-4:clause-1":
        [t("Hỗ trợ đầu tư", "hình thức hỗ trợ", ACTION)],
    "vn:law:2007:38-2007-qd-ubnd:ubnd-thanh-pho-ha-noi:article-1:clause-7~3": NOTHING,
    "vn:law:2020:08-2020-qd-ubnd:ubnd-tinh-thanh-hoa:annex-1:article-3:clause-2":
        [t("Chợ tạm", "chợ", PLACE)],
    "vn:law:2016:33-2016-qd-ubnd:ubnd-thanh-pho-da-nang:annex-1:article-2:clause-5":
        [t("Sản xuất sạch hơn trong công nghiệp", "việc áp dụng các giải pháp", ACTION)],
    "vn:law:2018:25-2018-qd-ubnd:ubnd-tinh-binh-phuoc:annex-1:article-3:clause-2":
        [t("Cơ sở chăn nuôi hỗn hợp", "cơ sở", PLACE)],
    "vn:law:2011:58-2011-qd-ubnd:ubnd-tinh-lam-dong:annex-1:article-2:clause-8":
        [t("Đơn vị thi công khai thác gỗ", "Đơn vị trúng thầu thi công khai thác gỗ", ACTOR, role=True)],
    "vn:law:2007:117-2007-qd-ubnd:ubnd-tinh-nghe-an:annex-1:article-2":
        [t("Khu công nghiệp nhỏ", "nơi tập trung các doanh nghiệp", PLACE, aliases=["KCN nhỏ"])],
    "vn:law:2009:06-2009-qd-ubnd:ubnd-tinh-binh-thuan:annex-1:article-3:clause-3":
        [t("Thuyền viên", "người làm việc", ACTOR)],
    "vn:law:2006:272-2006-qd-ttg:article-2:clause-9":
        [t("Ý kiến pháp lý", "văn bản", ARTIFACT)],
    "vn:law:2025:94-2025-qd-ubnd:ubnd-tinh-lai-chau:annex-1:article-3:clause-5":
        [t("Văn bản đến", "tất cả các loại văn bản", ARTIFACT, subtypes=[
        "văn bản quy phạm pháp luật", "văn bản hành chính", "văn bản chuyên ngành"])],
    "vn:law:2014:52-2014-qd-ubnd:ubnd-thanh-pho-da-nang:annex-1:article-3:clause-2":
        [t("Khu công nghệ thông tin tập trung", "các khu tập trung các hoạt động", PLACE)],
    "vn:law:2009:68-2009-qd-ubnd:ubnd-tinh-lam-dong:annex-1:article-3:clause-13":
        [t("Hành nghề khoan nước dưới đất quy mô vừa", "hành nghề khoan", ACTION)],
    "vn:law:2014:32-2014-qd-ubnd:ubnd-tinh-ben-tre:annex-1:article-3:clause-1":
        [t("Đường bao công trình", "ranh giới", PLACE)],
    "vn:law:2005:53-2005-qd-bgtvt:article-2:clause-12":
        [t("Ánh sáng chớp dài", "ánh sáng chớp", OTHER)],
    "vn:law:2013:05-2013-qd-ubnd:ubnd-thanh-pho-da-nang:annex-1:article-3:clause-4":
        [t("Hành lang an toàn đường bộ", "dải đất", PLACE)],
    "vn:law:2017:08-2017-qd-ubnd:ubnd-tinh-hoa-binh:annex-1:article-3:clause-11":
        [t("Cơ quan chuyên môn về xây dựng trực thuộc Ủy ban nhân dân tỉnh", "", BODY, subtypes=[
        "Sở Xây dựng", "Sở Giao thông vận tải", "Sở Công thương",
        "Sở Nông nghiệp và Phát triển nông thôn"])],
    "vn:law:2014:23-2014-qd-ubnd:ubnd-thanh-pho-da-nang:annex-1:article-3:clause-5":
        [t("Nhân viên tuần đường", "người", ACTOR, role=True)],
    "vn:law:2024:50-2024-qd-ubnd:ubnd-tinh-tuyen-quang:annex-1:article-3:clause-2":
        [t("Dịch vụ đích", "các ứng dụng, dịch vụ", THING)],
    "vn:law:2008:03-2008-qd-ubnd:ubnd-tinh-ba-ria-vung-tau:annex-1:article-2:clause-9":
        [t("Người chỉ huy nổ mìn", "người", ACTOR, role=True)],
    "vn:law:2014:46-2014-qd-ubnd:ubnd-thanh-pho-da-nang:annex-1:article-3:clause-6":
        [t("Năm tròn", "số năm kỷ niệm", TIME),
          t("Năm lẻ 5", "số năm kỷ niệm", TIME),
          t("Năm khác", "số năm kỷ niệm", TIME)],
    "vn:law:2005:58-2005-qd-bgtvt:annex-1:article-2:clause-3":
        [t("Phương tiện chuyên dùng", "", THING, subtypes=[
        "ôtô ray", "goòng máy", "cần trục", "máy chèn đường", "máy kiểm tra đường"])],
    "vn:law:2008:68-2008-qd-ubnd:ubnd-tinh-binh-duong:annex-1:article-3:clause-1":
        [t("Môi trường", "các yếu tố tự nhiên và vật chất nhân tạo", OTHER)],
    "vn:law:2024:59-2024-qd-ubnd:ubnd-tinh-an-giang:annex-1:article-3:clause-4":
        [t("Xâm phạm ATTT mạng", "", ACTION, ref="Luật An toàn thông tin mạng")],
    "vn:law:2015:47-2015-qd-ttg:article-3:clause-7":
        [t("Xã đặc biệt khó khăn, xã biên giới, xã an toàn khu", "các xã", PLACE)],
    "vn:law:2014:25-2014-qd-ubnd:ubnd-tinh-ba-ria-vung-tau:annex-1:article-3:clause-7":
        [t("Người chơi trò chơi điện tử", "cá nhân", ACTOR, role=True)],
    "vn:law:2014:05-2014-qd-ubnd:ubnd-tinh-hau-giang:annex-1:article-3:clause-11":
        [t("Cuộc họp của Chủ tịch UBND", "cuộc họp", ACTION)],
    "vn:law:2024:39-2024-qd-ubnd:ubnd-tinh-ha-tinh:annex-1:article-3:clause-6":
        [t("Đơn vị trực thuộc", "đơn vị", ACTOR, role=True)],
    "vn:law:2024:55-2024-qd-ubnd:ubnd-tinh-ca-mau:annex-1:article-3:clause-2":
        [t("Cổng truy xuất nguồn gốc sản phẩm tỉnh Cà Mau", "hệ thống", THING,
            aliases=["Cổng truy xuất nguồn gốc"])],
    "vn:law:2019:21-2019-qd-ubnd:ubnd-tinh-tay-ninh:annex-1:article-2:clause-6":
        [t("Sản xuất nông nghiệp hữu cơ", "hệ thống quá trình sản xuất", ACTION,
            aliases=["sản xuất hữu cơ"])],
    "vn:law:2014:47-2014-qd-ubnd:ubnd-tinh-thai-nguyen:annex-1:article-3:clause-9":
        [t("Chất thải nguy hại", "chất thải", THING)],
    "vn:law:2021:24-2021-qd-ubnd:ubnd-tinh-thai-nguyen:annex-1:article-3:clause-3":
        [t("Cơ sở dữ liệu tài liệu lưu trữ", "tập hợp các dữ liệu", ARTIFACT)],
    "vn:law:2024:07-2024-qd-ubnd:ubnd-tinh-binh-thuan:annex-1:article-3:clause-1":
        [t("Văn bản điện tử", "văn bản", ARTIFACT)],
    "vn:law:2009:31-2009-qd-ubnd:ubnd-thanh-pho-da-nang:annex-1:article-3:clause-9":
        [t("Tập thể nhỏ", "đơn vị", ACTOR)],
    "vn:law:2007:47-2007-qd-ubnd:ubnd-tinh-quang-nam:annex-1:article-2:clause-2":
        [t("Sản phẩm thủy sản sơ chế", "sản phẩm thủy sản", THING)],
    "vn:law:2020:17-2020-qd-ubnd:ubnd-tinh-quang-binh:annex-1:article-3:clause-7":
        [t("Khuyên đỡ bó cáp", "một kết cấu", THING)],
    "vn:law:2013:10-2013-qd-ubnd:ubnd-tinh-lai-chau:annex-1:article-3:clause-3":
        [t("Hoạt động đo đạc và bản đồ", "", ACTION, subtypes=[
        "Các thể loại đo đạc",
        "thành lập, xuất bản, phát hành các sản phẩm bản đồ",
        "lưu trữ, cấp phát, trao đổi, thu nhận, truyền dẫn, phổ cập những thông tin, tư liệu đo đạc và bản đồ",
        "nghiên cứu, phát triển, ứng dụng và chuyển giao công nghệ đo đạc và bản đồ"])],
    "vn:law:2022:16-2022-qd-ubnd:ubnd-tinh-ha-giang:annex-1:article-3:clause-2":
        NOTHING,
    "vn:law:2010:36-2010-qd-ubnd:ubnd-tinh-thua-thien-hue:annex-1:article-3:clause-16":
        [t("Điểm đấu nối", "các điểm", PLACE)],
    "vn:law:2012:06-2012-qd-ubnd:ubnd-thanh-pho-can-tho:annex-1:article-3:clause-7":
        [t("MPLS", "một phương thức", RULE,
            aliases=["Multiprotocol Label Switching", "Chuyển mạch nhãn đa giao thức"])],
    "vn:law:2014:30-2014-qd-ubnd:ubnd-tinh-hoa-binh:annex-1:article-2:clause-4":
        [t("Áp dụng sáng kiến lần đầu", "việc áp dụng sáng kiến", ACTION)],
    "vn:law:2013:28-2013-qd-ubnd:ubnd-tinh-thua-thien-hue:annex-1:article-3:clause-3":
        [t("Cơ quan quản lý thực hiện Chương trình MTQG của tỉnh", "các Sở, ban ngành", BODY, role=True)],
    "vn:law:2023:06-2023-tt-bkhcn:article-2:clause-4":
        [t("Tuyển chọn tổ chức, cá nhân thực hiện nhiệm vụ khoa học và công nghệ cấp quốc gia",
            "việc", ACTION)],
    "vn:law:2026:03-2026-tt-bkhcn:article-3:clause-3":
        [t("Chuyên gia đánh giá Giải thưởng chất lượng quốc gia", "", ACTOR)],
    "vn:law:2011:40-2011-tt-nhnn:article-2:clause-1":
        [t("Giấy phép", "", ARTIFACT, subtypes=[
        "Giấy phép thành lập và hoạt động của ngân hàng thương mại",
        "Giấy phép thành lập chi nhánh ngân hàng nước ngoài",
        "Giấy phép thành lập văn phòng đại diện"])],
    "vn:law:2012:51-2012-tt-bgtvt:article-2:clause-49":
        [t("Đường hàng không", "khu vực", PLACE, aliases=["Airway"])],
    "vn:law:2011:13-2011-tt-bnnptnt:article-4:clause-3":
        [t("Vi phạm nghiêm trọng qui định ATTP", "", STATUS)],
    "vn:law:2019:30-2019-tt-btc:article-2:clause-8":
        [t("Tổ chức mở tài khoản trực tiếp", "tổ chức", ACTOR, role=True)],
    "vn:law:2018:45-2018-tt-bct:article-3:clause-55":
        [t("Nút giao dịch", "vị trí", PLACE)],
    "vn:law:2025:16-2025-tt-bct:article-3:clause-44":
        [t("Hệ số tải trung bình tháng", "tỷ lệ", AMOUNT)],
    "vn:law:2017:19-2017-tt-bgtvt:article-4:clause-47":
        [t("Giai đoạn hồ nghi", "thời gian", TIME, aliases=["Uncertainty phase"])],
    "vn:law:2016:25-2016-tt-bct:article-3:clause-33":
        [t("Lưới điện phân phối", "phần lưới điện", THING)],
    "vn:law:2019:03-2019-tt-bct:article-3:clause-9":
        [t("Vật liệu đóng gói và bao bì đóng gói để vận chuyển", "hàng hóa", THING)],
    "vn:law:2024:29-2024-tt-bct:article-3:clause-3":
        [t("Túi nhựa", "túi nylon", THING)],
    "vn:law:2013:35-2013-tt-bgtvt:article-3:clause-2":
        [t("Đơn vị kinh doanh vận tải hàng hóa bằng xe ô tô", "", ACTOR, role=True)],
    "vn:law:2024:54-2024-tt-bgtvt:article-3:clause-3":
        [t("Phụ tùng chưa qua sử dụng", "phụ tùng", THING)],
    "vn:law:2019:03-2019-tt-bgtvt:article-3:clause-5":
        [t("Nhà thầu bảo trì công trình đường bộ", "các tổ chức, cá nhân", ACTOR, role=True)],
    "vn:law:2018:07-2018-tt-bgtvt:article-3:clause-11":
        [t("Tàu biển dưới công ước", "tàu biển", THING)],
    "vn:law:2016:25-2016-tt-bct:article-3:clause-11":
        [t("Điều độ hệ thống điện", "hoạt động chỉ huy, điều khiển", ACTION)],
    "vn:law:2022:08-2022-tt-bgddt:annex-1:article-2:clause-16":
        [t("Hệ thống hỗ trợ tuyển sinh chung của Bộ Giáo dục và Đào tạo", "hệ thống phần mềm", THING)],
    "vn:law:2026:29-2026-tt-bct:article-3:clause-77":
        [t("Sản lượng điện hợp đồng", "sản lượng điện năng", AMOUNT)],
    "vn:law:2020:119-2020-tt-bca:article-2:clause-2":
        [t("L", "Chiều dài", AMOUNT)],
    "vn:law:2012:135-2012-tt-btc:article-3:clause-3":
        [t("Giá bán", "giá", AMOUNT)],
    "vn:law:2016:25-2016-tt-bct:article-3:clause-8":
        [t("Dự phòng quay", "khả năng", AMOUNT)],
    "vn:law:2009:36-2009-tt-bnnptnt:article-2:clause-8":
        [t("Tác nhân gây bệnh", "các yếu tố gây bệnh", THING)],
    "vn:law:2014:03-2014-tt-bgtvt:article-3:clause-1":
        [t("Autopilot", "Hệ thống lái tự động", THING)],
    "vn:law:2010:17-2010-tt-bkh:article-3:clause-5":
        [t("Văn bản điện tử", "văn bản", ARTIFACT)],
    "vn:law:2021:12-2021-tt-bnnptnt:article-3:clause-2":
        [t("Xử lý chất thải chăn nuôi", "việc áp dụng", ACTION)],
    "vn:law:2017:19-2017-tt-bgtvt:article-4:clause-81":
        [t("Rủi ro an toàn", "khả năng", CONDITION)],
    "vn:law:2019:22-2019-tt-nhnn:article-3:clause-3":
        [t("Kinh doanh bất động sản", "việc bỏ vốn đầu tư", ACTION)],
    "vn:law:2025:48-2025-tt-bkhcn:article-2:clause-12":
        [t("Địa chỉ Internet", "địa chỉ", OTHER, aliases=["địa chỉ IP"])],
    "vn:law:2017:02-2017-tt-nhnn:article-3:clause-1":
        [t("Bên bán hàng", "bên bán hàng hóa", ACTOR, role=True)],
    "vn:law:2019:12-2019-tt-bct:article-3:clause-11":
        [t("Quy tắc cụ thể mặt hàng", "quy tắc", RULE)],
    "vn:law:2016:49-2016-tt-btnmt:article-3:clause-2":
        [t("Kiểm tra công trình, sản phẩm trong lĩnh vực quản lý đất đai", "việc", ACTION)],
    "vn:law:2013:09-2013-tt-bkhcn:article-2:clause-3":
        [t("Bản đồ công nghệ", "bộ tài liệu", ARTIFACT)],
    "vn:law:2024:02-2024-tt-bkhcn:article-3:clause-7":
        [t("Dữ liệu truy xuất nguồn gốc", "các dữ liệu", ARTIFACT)],
    "vn:law:2021:22-2021-tt-bgddt:article-2:clause-2":
        [t("Đánh giá thường xuyên", "hoạt động đánh giá", ACTION)],
    "vn:law:2012:18-2012-tt-bnnptnt:article-2:clause-1":
        [t("Cây công nghiệp và cây ăn quả lâu năm", "những loài cây", THING)],
    "vn:law:2023:29-2023-tt-btnmt:article-3:clause-5":
        [t("Bảo dưỡng công trình, phương tiện đo", "các hoạt động", ACTION)],
    "vn:law:2016:107-2016-tt-btc:article-2:clause-3":
        [t("Tổ chức phát hành chứng khoán cơ sở", "tổ chức phát hành chứng khoán", ACTOR, role=True)],
    "vn:law:2018:26-2018-tt-nhnn:article-3:clause-1":
        [t("Điều tra thống kê tiền tệ và ngân hàng ngoài Chương trình điều tra thống kê quốc gia",
            "", ACTION, subtypes=[
        "điều tra thống kê tiền tệ và ngân hàng của Ngân hàng Nhà nước",
        "điều tra thống kê tiền tệ và ngân hàng ngoài thống kê nhà nước"])],
    "vn:law:2019:12-2019-tt-bkhcn:article-3:clause-1":
        [t("Sản phẩm, thiết bị sử dụng nước", "sản phẩm, thiết bị", THING)],
    "vn:law:2012:53-2012-tt-bgddt:article-2:clause-1":
        [t("Trang thông tin điện tử", "nơi cung cấp, trao đổi thông tin", THING)],
    "vn:law:2024:41-2024-tt-nhnn:article-3:clause-1":
        [t("Các hệ thống thanh toán quan trọng", "", THING, subtypes=[
        "Hệ thống TTLNH Quốc gia", "hệ thống thanh toán ngoại tệ",
        "hệ thống thanh toán tiền giao dịch chứng khoán",
        "hệ thống bù trừ, chuyển mạch giao dịch tài chính"])],
    "vn:law:2016:17-2016-tt-bldtbxh:article-2:clause-2":
        [t("Hộ thoát nghèo", "hộ nghèo", ACTOR)],
    "vn:law:2017:49-2017-tt-btnmt:article-3:clause-3":
        [t("Đánh giá hiện trạng vùng bờ", "đánh giá hiện trạng", ACTION)],
    "vn:law:2025:16-2025-tt-bnnmt:article-3:clause-12":
        [t("Rừng tre nứa", "rừng", PLACE), t("Rừng cau dừa", "rừng", PLACE)],
    "vn:law:2019:03-2019-tt-bqp:article-3:clause-2":
        [t("Định hướng chính trị, tư tưởng trong nội dung thông tin trên báo chí",
            "hoạt động", ACTION)],
    "vn:law:2018:38-2018-tt-bct:article-3:clause-1":
        [t("Chứng nhận xuất xứ hàng hóa theo GSP", "việc thương nhân khai báo", ACTION)],
    "vn:law:2019:10-2019-tt-bkhcn:article-3:clause-3":
        [t("Đơn vị quản lý nhiệm vụ Nghị định thư", "đơn vị", BODY, role=True)],
    "vn:law:2015:58-2015-tt-bqp:annex-1:article-3:clause-7":
        [t("Thành viên mạng lưới", "các cơ quan, đơn vị, tổ chức", ACTOR, role=True)],
    "vn:law:2018:38-2018-tt-bct:article-3:clause-2":
        [t("Chứng từ chứng nhận xuất xứ hàng hóa theo GSP", "chứng từ", ARTIFACT)],
    "vn:law:2012:17-2012-tt-bvhttdl:article-3:clause-3":
        [t("Kết hợp với loại hình nghệ thuật khác", "hình thức sử dụng", ACTION)],
    "vn:law:2014:40-2014-tt-bct:article-3:clause-32":
        [t("Lệnh điều độ", "lệnh chỉ huy", ARTIFACT)],
    "vn:law:2018:32-2018-tt-btnmt:article-3:clause-1":
        [t("Danh sách thông tin, dữ liệu tài nguyên và môi trường",
            "liệt kê các đối tượng thông tin", ARTIFACT)],
    "vn:law:2026:14-2026-tt-bca:article-3:clause-32":
        [t("Tàu bay không khai thác", "tàu bay", THING)],
    "vn:law:2012:26-2012-tt-bgtvt:article-3:clause-1":
        [t("Vị trí nguy hiểm", "vị trí", PLACE)],
    "vn:law:2026:14-2026-tt-bca:article-3:clause-19":
        [t("Khu vực cách ly", "một phần của khu vực hạn chế", PLACE)],
    "vn:law:2013:52-2013-tt-bgtvt:article-2:clause-8":
        [t("Nhà thầu bảo trì công trình đường bộ", "các tổ chức, cá nhân", ACTOR, role=True)],
    "vn:law:2018:45-2018-tt-bct:article-3:clause-30":
        [t("Giá thị trường điện toàn phần", "tổng giá điện năng thị trường", AMOUNT)],
    "vn:law:2014:24-2014-tt-bnnptnt:article-3:clause-2":
        [t("Chợ đấu giá nông sản", "nơi", PLACE)],
    "vn:law:2010:17-2010-tt-bkh:article-3:clause-3":
        [t("Cơ quan vận hành Hệ thống", "Cục Quản lý đấu thầu", BODY)],
    "vn:law:2010:07-2010-tt-bgddt:article-2:clause-6":
        [t("Tên-miền", "tên đăng ký sở hữu", OTHER, aliases=["Domain name"])],
    "vn:law:2024:86-2024-tt-btc:article-3:clause-5":
        [t("Đơn vị phụ thuộc của tổ chức kinh tế, tổ chức khác",
            "chi nhánh, văn phòng đại diện", ACTOR)],
    "vn:law:2019:22-2019-tt-nhnn:article-3:clause-14":
        [t("Người có liên quan của một tổ chức, cá nhân", "tổ chức, cá nhân", ACTOR, role=True)],
    "vn:law:2022:02-2022-tt-bgddt:article-2:clause-3":
        [t("Nhóm ngành đào tạo", "tập hợp một số ngành đào tạo", OTHER)],
    "vn:law:2012:09-2012-ttlt-bca-bqp-tandtc-vksndtc:article-3:clause-2":
        [t("Lập công", "trường hợp", STATUS)],
    "vn:law:2009:14-2009-ttlt-bct-btc:article-3:clause-5":
        [t("Cơ quan quản lý xuất khẩu", "cơ quan có thẩm quyền", BODY, role=True)],
    "vn:law:2026:68-2026-vbhn-nd-bct:article-2:clause-7":
        [t("Thông số đầu vào cơ bản trong khâu phát điện", "các yếu tố", AMOUNT)],
}

# The pairs a human decided about, drawn from clauses that landed in the same
# sample. They are the only pairs in the gold set: a pair nobody looked at is
# not evidence either way.
PAIRS = [
    ("vn:law:2019:03-2019-tt-bgtvt:article-3:clause-5",
     "vn:law:2013:52-2013-tt-bgtvt:article-2:clause-8", "same",
     "Cùng chỉ tổ chức, cá nhân thực hiện bảo trì công trình đường bộ theo hợp đồng "
     "với cơ quan quản lý đường bộ, chỉ khác cách diễn đạt."),
    ("vn:law:2015:28-2015-qd-ubnd:ubnd-tinh-phu-yen:annex-1:article-2:clause-2",
     "vn:law:2010:36-2010-qd-ubnd:ubnd-tinh-thua-thien-hue:annex-1:article-3:clause-16", "same",
     "Cả hai đều là điểm xả nước thải của hộ thoát nước vào hệ thống thoát nước chung."),
    ("vn:law:2024:07-2024-qd-ubnd:ubnd-tinh-binh-thuan:annex-1:article-3:clause-1",
     "vn:law:2010:17-2010-tt-bkh:article-3:clause-5", "same",
     "Cùng là văn bản dưới dạng thông điệp dữ liệu, hai thông tư diễn đạt khác nhau."),
    ("vn:law:2015:73-2015-qd-ubnd:ubnd-tinh-ninh-thuan:annex-1:article-3:clause-11",
     "vn:law:2014:05-2014-qd-ubnd:ubnd-tinh-hau-giang:annex-1:article-3:clause-11", "broader",
     "Cuộc họp của Ủy ban nhân dân rộng hơn cuộc họp do Chủ tịch chủ trì."),
    ("vn:law:2007:77-2007-qd-ubnd:ubnd-tinh-binh-thuan:annex-1:article-2:clause-5",
     "vn:law:2016:34-2016-qd-ubnd:ubnd-tinh-yen-bai:annex-1:article-4:clause-5", "differs",
     "Người có thẩm quyền là một cá nhân quyết định dự án, cơ quan nhà nước có thẩm quyền "
     "là pháp nhân ký kết hợp đồng, khác chủ thể."),
    ("vn:law:2025:72-2025-nd-cp:article-2:clause-2",
     "vn:law:2017:02-2017-tt-nhnn:article-3:clause-1", "differs",
     "Bên bán điện chỉ bán điện năng theo hợp đồng mua bán điện, bên bán hàng là bên bán "
     "hàng hóa, dịch vụ nói chung trong hóa đơn."),
    ("vn:law:2024:28-2024-nq-hdnd:hdnd-tinh-an-giang:article-2",
     "vn:law:2008:03-2008-qd-ubnd:ubnd-tinh-bac-lieu:annex-1:article-2:clause-6", "differs",
     "Khu dân cư là nơi người dân sinh sống, khu sản xuất, kinh doanh, dịch vụ tập trung "
     "là khu công nghiệp và tương đương."),
    ("vn:law:2007:77-2007-qd-ubnd:ubnd-tinh-binh-thuan:annex-1:article-2:clause-7",
     "vn:law:2018:10-2018-qd-ubnd:ubnd-tinh-quang-binh:annex-1:article-3:clause-5", "differs",
     "Chủ đầu tư nắm vốn của dự án, chủ sở hữu cột treo cáp nắm một tài sản hạ tầng cụ thể."),
]

ANNOTATOR = "tamnd"
ANNOTATED_AT = "2026-08-01T00:00:00Z"


def slug(s):
    s = unicodedata.normalize("NFC", s).lower()
    return "".join(c for c in s if c.isalnum() or c.isspace())


def main():
    data = os.environ.get("LUATDO_DATA") or os.path.join(
        os.path.expanduser("~"), "data", "luatdo")
    d = os.path.join(data, "concept")
    with open(os.path.join(d, "gold_candidates.jsonl"), encoding="utf-8") as f:
        rows = [json.loads(line) for line in f if line.strip()]

    drawn = [row["unit_id"] for row in rows]
    if len(set(drawn)) != len(drawn):
        sys.exit("the draw contains a clause identifier twice, which the anchor stage must not produce")
    missing = [u for u in drawn if u not in ANNOTATIONS]
    stale = [u for u in ANNOTATIONS if u not in set(drawn)]
    if missing or stale:
        sys.exit("\n".join(
            ["the draw and the annotations no longer describe the same clauses"]
            + ["  drawn and not annotated: " + u for u in missing]
            + ["  annotated and not drawn: " + u for u in stale]))

    problems, gold = [], []
    for row in rows:
        terms = ANNOTATIONS[row["unit_id"]]
        low = slug(row["text"])
        for term in terms:
            for field in ("genus",):
                if term[field] and slug(term[field]) not in low:
                    problems.append("%s %s: %r is not in the clause"
                                    % (row["unit_id"], field, term[field]))
            for sub in term["enumerated_subtypes"]:
                if slug(sub) not in low:
                    problems.append("%s subtype: %r is not in the clause" % (row["unit_id"], sub))
        row["defines_nothing"] = not terms
        row["terms"] = terms
        row["annotated_by"] = ANNOTATOR
        row["annotated_at"] = ANNOTATED_AT
        gold.append(row)

    if problems:
        sys.exit("\n".join(problems))

    with open(os.path.join(d, "gold.jsonl"), "w", encoding="utf-8") as f:
        for row in gold:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")

    pairs = []
    by_unit = {row["unit_id"]: row for row in rows}
    for a, b, verdict, rationale in PAIRS:
        ta, tb = ANNOTATIONS[a][0], ANNOTATIONS[b][0]
        pairs.append({
            "a": term_id(by_unit[a]["scope_id"], ta["label_vi"]),
            "b": term_id(by_unit[b]["scope_id"], tb["label_vi"]),
            "verdict": verdict,
            "rationale": rationale,
            "annotated_by": ANNOTATOR,
            "annotated_at": ANNOTATED_AT,
        })
    with open(os.path.join(d, "gold_pairs.jsonl"), "w", encoding="utf-8") as f:
        for pair in pairs:
            f.write(json.dumps(pair, ensure_ascii=False) + "\n")

    defined = sum(len(row["terms"]) for row in gold)
    nothing = sum(1 for row in gold if row["defines_nothing"])
    print("wrote %d clauses, %d terms, %d clauses that define nothing, %d pairs"
          % (len(gold), defined, nothing, len(pairs)))
    kinds = {}
    for row in gold:
        for term in row["terms"]:
            kinds[term["kind"]] = kinds.get(term["kind"], 0) + 1
    for kind in sorted(kinds, key=lambda k: -kinds[k]):
        print("  %-10s %3d" % (kind, kinds[kind]))
    roles = sum(1 for row in gold for term in row["terms"] if term["is_role"])
    refs = sum(1 for row in gold for term in row["terms"] if term["defines_by_reference"])
    enums = sum(1 for row in gold for term in row["terms"] if term["enumerated_subtypes"])
    print("  roles %d, by reference %d, enumerations %d" % (roles, refs, enums))


def term_id(scope, label):
    return "vn:term:" + scope + ":" + go_slug(label)


def go_slug(s):
    """Mirror law.Slug so the identifiers match the ones Go mints."""
    s = unicodedata.normalize("NFD", s.lower())
    out, dash = [], False
    for c in s:
        if unicodedata.combining(c):
            continue
        if c == "đ":
            out.append("d")
            dash = False
        elif c.isalnum() and c.isascii():
            out.append(c)
            dash = False
        elif not dash and out:
            out.append("-")
            dash = True
    return "".join(out).strip("-")


if __name__ == "__main__":
    main()
