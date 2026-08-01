package subject

import (
	"strings"
	"testing"

	"github.com/tamnd/luatdo/law"
)

func TestVocabularyLoadsAndIsWellFormed(t *testing.T) {
	v, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	domains := v.Domains()
	if len(domains) < 15 || len(domains) > 30 {
		t.Errorf("got %d domains, want roughly twenty", len(domains))
	}
	subdomains := len(v.Subjects) - len(domains)
	if subdomains < 120 || subdomains > 180 {
		t.Errorf("got %d subdomains, want roughly a hundred and fifty", subdomains)
	}
	for _, s := range v.Subjects {
		if s.LabelVI == "" || s.LabelEN == "" {
			t.Errorf("subject %s is missing a label", s.ID)
		}
		if s.IsDomain() {
			if len(v.Children(s.ID)) == 0 {
				t.Errorf("domain %s has no subdomains", s.ID)
			}
			continue
		}
		if !strings.HasPrefix(s.ID, s.Parent+"/") {
			t.Errorf("subdomain %s does not sit under its parent %s in its identifier", s.ID, s.Parent)
		}
		if len(s.Cues) == 0 {
			t.Errorf("subdomain %s has no cues, so nothing can ever land in it", s.ID)
		}
	}
}

// A cue that is a single very common word fires on everything. That is a
// mistake in the vocabulary rather than in the matcher, so the vocabulary is
// what gets checked.
func TestNoCueIsATooCommonWord(t *testing.T) {
	v := MustLoad()
	// These words appear in a large fraction of Vietnamese legal titles and
	// carry no subject at all. A cue equal to one of them would file most of
	// the corpus under one domain.
	banned := map[string]bool{
		"quy dinh": true, "quyet dinh": true, "ban hanh": true, "thong tu": true,
		"nghi dinh": true, "quy che": true, "ke hoach": true, "to chuc": true,
		"quan ly": true, "thuc hien": true, "nha nuoc": true, "tinh": true,
		"huyen": true, "xa": true, "chinh phu": true, "bo": true, "so": true,
	}
	for _, s := range v.Subjects {
		for _, c := range s.folded {
			word := strings.TrimSpace(c.text)
			if banned[word] {
				t.Errorf("subject %s uses %q as a cue, which fires on most of the corpus", s.ID, word)
			}
		}
	}
}

// A cue of one syllable can never reach the threshold on its own, so it is
// either dead weight or a promise the vocabulary does not keep. Both are worth
// failing on, because a reader adding "rừng" to the forestry subdomain has
// every reason to think it will fire and it never will.
func TestNoCueIsTooShortToEverFire(t *testing.T) {
	v := MustLoad()
	for _, s := range v.Subjects {
		for _, c := range s.folded {
			if c.words < minScore {
				t.Errorf("subject %s has cue %q of %d words, which cannot reach the threshold of %d",
					s.ID, strings.TrimSpace(c.text), c.words, minScore)
			}
		}
	}
}

// gold is a hand annotated set of real corpus documents. Each was drawn from
// the parsed corpus at random, read, and filed under the subdomain a person
// would put it in. Real titles are what makes this worth measuring: they are
// misspelled, they run to four lines, half of them amend another instrument
// rather than say anything themselves, and a set of titles invented alongside
// the vocabulary would measure nothing but the vocabulary agreeing with itself.
//
// A miss stays in the table rather than being fixed by relabelling. Entries the
// method has no chance on, such as a decision that only repeals another one by
// number, are left where they are, because the recall figure is meant to be the
// real one.
var gold = []struct {
	title   string
	docType string
	body    string
	want    string
}{
	{"Thông tư số 13/2019/TT-BYT sửa đổi, bổ sung một số điều của Thông tư số 39/2018/TT-BYT quy định thống nhất giá dịch vụ khám bệnh, chữa bệnh bảo hiểm y tế giữa các bệnh viện cùng hạng trong toàn quốc", "Thông tư", "Bộ Y tế", "y-te/gia-dich-vu-y-te"},
	{"Thông tư số 131/2016/TT-BTC Quy định mức thu, chế độ thu, nộp, quản lý và sử dụng phí sử dụng đường bộ trạm thu phí Bàn Thạch quốc lộ 1", "Thông tư", "Bộ Tài chính", "tai-chinh-cong/phi-le-phi"},
	{"Quyết định số 51/2015/QĐ-UBND Về việc ban hanh quy định chức năng, nhiệm vụ, quyền hạn và cơ cấu tổ chức của Thanh tra tỉnh Đồng Tháp", "Quyết định", "UBND Tỉnh Đồng Tháp", "bo-may-nha-nuoc/to-chuc-co-quan"},
	{"Nghị quyết số 38/2003/NQ-HĐND Về mức thu phí, chế độ thu, nộp, quản lý và sử dụng đối với Phí vệ sinh trên địa bàn thị xã Phủ Lý", "Nghị quyết", "HĐND tỉnh Hà Nam", "tai-chinh-cong/phi-le-phi"},
	{"Quyết định số 53/2015/QĐ-UBND Về việc thay thế Phụ lục IV - Bảng giá động vật rừng ban hành kèm theo Quyết định số 47/2014/QĐ-UBND của Ủy ban nhân dân Thành phố", "Quyết định", "UBND Thành phố Hồ Chí Minh", "nong-nghiep/lam-nghiep"},
	{"Quyết định số 115/2003/QĐ-BBCVT Ban hành tạm thời cước dịch vụ điện thoại di động trả sau CDMA gói dịch vụ Vip", "Quyết định", "Bộ Bưu chính, Viễn thông", "thong-tin-truyen-thong/vien-thong"},
	{"Pháp lệnh số 29/2000/PL-UBTVQH10 Thủ đô Hà Nội", "Pháp lệnh", "Uỷ ban Thường vụ Quốc hội", "bo-may-nha-nuoc/chinh-quyen-dia-phuong"},
	{"Quyết định số 51/2015/QĐ-UBND Ban hành Quy định chức năng, nhiệm vụ, quyền hạn và cơ cấu tổ chức của Sở Giao thông vận tải tỉnh Cà Mau", "Quyết định", "UBND Tỉnh Cà Mau", "bo-may-nha-nuoc/to-chuc-co-quan"},
	{"Quyết định số 29/2015/QĐ-UBND Quy định chức năng, nhiệm vụ, quyền hạn và cơ cấu tổ chức của Ban Quản lý các khu chế xuất và công nghiệp Cần Thơ", "Quyết định", "UBND Thành phố Cần Thơ", "bo-may-nha-nuoc/to-chuc-co-quan"},
	{"Quyết định số 370/2003/QĐ-BLĐTBXH Về việc Ban hành Quy chế bổ nhiệm, công nhận, bổ nhiệm lại, từ chức, miễn nhiệm Hiệu trưởng, Phó hiệu trưởng Trường dạy nghề và Giám đốc Trung tâm dạy nghề", "Quyết định", "Bộ Lao động - Thương binh và Xã hội", "lao-dong/giao-duc-nghe-nghiep"},
	{"Quyết định số 61/2003/QĐ-UB Về việc phân cấp nguồn thu, nhiệm vụ chi giữa các cấp ngân sách", "Quyết định", "UBND tỉnh Cần Thơ", "tai-chinh-cong/ngan-sach"},
	{"Thông tư số 10/2018/TT-BTNMT Ban hành Danh mục địa danh dân cư, sơn văn, thủy văn, kinh tế - xã hội phục vụ công tác thành lập bản đồ phần đất liền tỉnh Cà Mau", "Thông tư", "Bộ Tài nguyên và Môi trường", "tai-nguyen-moi-truong"},
	{"Quyết định số 377/2003/QĐ-UB V/v cho phép thành lập Trung tâm Dạy nghề cho người tàn tật trực thuộc Hội Chữ thập đỏ huyện Vũ Thư", "Quyết định", "UBND tỉnh Thái Bình", "lao-dong/giao-duc-nghe-nghiep"},
	{"Quyết định số 380/1997/QĐ-NHNN1 quy định về trạng thái đồng Việt Nam đối với các chi nhánh Ngân hàng nước ngoài hoạt động tại Việt Nam", "Quyết định", "Ngân hàng Nhà nước Việt Nam", "ngan-hang-tai-chinh/to-chuc-tin-dung"},
	{"Thông tư số 50/2015/TT-BYT Quy định việc kiểm tra vệ sinh, chất lượng nước ăn uống, nước sinh hoạt", "Thông tư", "Bộ Y tế", "y-te"},
	{"Quyết định số 41/2019/QĐ-UBND Ban hành mức thu tiền sử dụng khu vực biển đối với hoạt động khai thác, sử dụng tài nguyên biển trên địa bàn tỉnh Bà Rịa Vũng Tàu năm 2020", "Quyết định", "UBND tỉnh Bà Rịa - Vũng Tàu", "tai-nguyen-moi-truong/bien-hai-dao"},
	{"Nghị quyết số 06/2022/NQ-HĐND Quy định một số chính sách hỗ trợ phát triển giáo dục mầm non dân lập, tư thục trên địa bàn tỉnh Quảng Ninh", "Nghị quyết", "HĐND Tỉnh Quảng Ninh", "giao-duc/giao-duc-pho-thong"},
	{"Quyết định số 649/1997/QĐ-UB V/v Ban hành Quy chế đấu thầu, thẩm định giá mua sắm đồ dùng trang thiết bị, phương tiện làm việc đối với cơ quan Nhà nước", "Quyết định", "UBND Tỉnh Hưng Yên", "tai-chinh-cong/dau-thau-mua-sam"},
	{"Quyết định số 42/2001/QĐ-UBND V/v quy định mức thu tại các cơ sở y tế công lập tỉnh Hưng Yên về viện phí, dịch vụ kỹ thuật, xét nghiệm, kiểm tra sức khoẻ", "Quyết định", "UBND Tỉnh Hưng Yên", "y-te/gia-dich-vu-y-te"},
	{"Nghị quyết số 01/2022/NQ-HĐND quy định một số chính sách hỗ trợ phát triển sản xuất nông nghiệp hàng hoá; hỗ trợ nâng cao năng lực cho khu vực kinh tế tập thể trên địa bàn tỉnh Bắc Kạn", "Nghị quyết", "HĐND tỉnh Bắc Kạn", "nong-nghiep/nong-thon-moi"},
	{"Quyết định số 56/2022/QĐ-UBND Sửa đổi, bổ sung một số điều của Quy định thực hiện cơ chế một cửa, một cửa liên thông trong giải quyết thủ tục hành chính trên địa bàn tỉnh Lai Châu", "Quyết định", "UBND Tỉnh Lai Châu", "bo-may-nha-nuoc/thu-tuc-hanh-chinh"},
	{"Quyết định số 31/2015/QĐ-UBND Ban hành Quy định tuyển chọn, giao trực tiếp tổ chức và cá nhân thực hiện nhiệm vụ khoa học và công nghệ cấp tỉnh sử dụng ngân sách nhà nước", "Quyết định", "UBND Tỉnh Khánh Hòa", "khoa-hoc-cong-nghe/nhiem-vu-khoa-hoc"},
	{"Quyết định số 05/2025/QĐ-UBND Quy định chức năng, nhiệm vụ, quyền hạn và cơ cấu tổ chức của Sở Khoa học và Công nghệ tỉnh Thái Bình", "Quyết định", "UBND tỉnh Thái Bình", "bo-may-nha-nuoc/to-chuc-co-quan"},
	{"Nghị định số 02/2000/NĐ-CP Về đăng ký kinh doanh", "Nghị định", "Chính phủ", "doanh-nghiep-dau-tu/dang-ky-doanh-nghiep"},
	{"Quyết định số 63/2003/QĐ-TTg Về việc phê duyệt Phương án tổng thể sắp xếp, đổi mới doanh nghiệp nhà nước trực thuộc Bộ Giao thông vận tải đến năm 2005", "Quyết định", "Thủ tướng Chính phủ", "doanh-nghiep-dau-tu/doanh-nghiep-nha-nuoc"},
	{"Nghị quyết số 29/2015/NQ-HĐND Quy định cơ cấu nguồn vốn thực hiện Chương trình kiên cố hóa kênh mương và giao thông nông thôn giai đoạn 2016 - 2020", "Nghị quyết", "HĐND tỉnh Đắk Nông", "nong-nghiep/thuy-loi-thien-tai"},
	{"Quyết định số 37/2003/QĐ-BNN Về việc ban hành quy trình vận hành điều tiết hồ chứa nước Phú Ninh tỉnh Quảng Nam", "Quyết định", "Bộ Nông nghiệp và Phát triển nông thôn", "nong-nghiep/thuy-loi-thien-tai"},
	{"Chỉ thị số 09/2003/CT-UB Về tăng cường quản lý chất thải rắn y tế", "Chỉ thị", "UBND Thành phố Hồ Chí Minh", "y-te"},
	{"Quyết định số 13/2021/QĐ-UBND Về việc quy định chức năng, nhiệm vụ và quyền hạn của Sở Tư pháp tỉnh Đắk Lắk", "Quyết định", "UBND Tỉnh Đắk Lắk", "bo-may-nha-nuoc/to-chuc-co-quan"},
	{"Quyết định số 358/2003/QĐ-BTP Về việc ban hành Quy chế cộng tác viên của tổ chức trợ giúp pháp lý", "Quyết định", "Bộ Tư pháp", "tu-phap-dan-su/xay-dung-phap-luat"},
	{"Quyết định số 6282/2003/QĐ-BYT Về việc ban hành Danh mục Vật tư tiêu hao y tế được Bảo hiểm xã hội (Bảo hiểm Y tế) thanh toán", "Quyết định", "Bộ Y tế", "an-sinh-xa-hoi/bao-hiem-y-te"},
	{"Quyết định số 379/1994/QĐ-UB Về việc bố trí cán bộ, chế độ sinh hoạt phí đối với cán bộ Đảng, Chính quyền và kinh phí hoạt động của các đoàn thể Nhân dân ở xã, phường, thị trấn", "Quyết định", "UBND Tỉnh Hà Tĩnh", "bo-may-nha-nuoc/can-bo-cong-chuc"},
	{"Quyết định số 270/1997/QĐ-KT-UB Về việc ban hành tiêu chuẩn xét tặng Kỷ niệm Huân chương Hùng Vương", "Quyết định", "UBND Tỉnh Phú Thọ", "bo-may-nha-nuoc/thi-dua-khen-thuong"},
	{"Quyết định số 2822/2015/QĐ-UBND Ban hành Quy định thực hiện nếp sống văn minh trong việc cưới, việc tang, lễ hội và một số lễ nghi, sinh hoạt cộng đồng khác trên địa bàn thành phố Hải Phòng", "Quyết định", "UBND Thành phố Hải Phòng", "van-hoa-the-thao-du-lich/gia-dinh-nep-song"},
	{"Thông tư số 123/2021/TT-BTC Sửa đổi khoản 2 Điều 4 Điều lệ tổ chức và hoạt động của Công ty trách nhiệm hữu hạn một thành viên Mua bán nợ Việt Nam", "Thông tư", "Bộ Tài chính", "doanh-nghiep-dau-tu/doanh-nghiep-nha-nuoc"},
	{"Quyết định số 36/2003/QĐ-BNN Về việc ban hành tiêu chuẩn trang thiết bị quản lý trong hệ thống công trình thuỷ lợi phục vụ tưới tiêu 14TCN 131-2002", "Quyết định", "Bộ Nông nghiệp và Phát triển nông thôn", "nong-nghiep/thuy-loi-thien-tai"},
	{"Nghị quyết số 05/2021/NQ-HĐND quy định tạm thời mức giá tạm thời đối với dịch vụ xét nghiệm SARS-CoV-2 trong các đơn vị sự nghiệp y tế công lập trên địa bàn tỉnh Vĩnh Phúc", "Nghị quyết", "HĐND tỉnh Vĩnh Phú", "y-te"},
	{"Thông tư số 36/1998/TT-BTC hướng dẫn chế độ tài chính đối với Trung tâm quản lý bay dân dụng Việt Nam", "Thông tư", "Bộ Tài chính", "giao-thong-van-tai/duong-sat-hang-khong"},
	{"Quyết định số 36/2017/QĐ-UBND sửa đổi, bổ sung một số điều Quy định tiêu chuẩn, điều kiện bổ nhiệm, bổ nhiệm lại công chức, viên chức giữ chức vụ từ Trưởng phòng, Phó Trưởng phòng và tương đương trở xuống", "Quyết định", "UBND Tỉnh Đồng Tháp", "bo-may-nha-nuoc/can-bo-cong-chuc"},
	{"Chỉ thị số 10/1999/CT-UB V/v Thực hiện công tác văn thư lưu trữ và bảo quản tài liệu lưu trữ trên địa bàn Tỉnh Bình Phước", "Chỉ thị", "UBND tỉnh Bình Phước", "bo-may-nha-nuoc/van-thu-luu-tru"},
	{"Quyết định số 16/2024/QĐ-UBND Sửa đổi, bổ sung một số điều của Quy chế phối hợp trong công tác quản lý người nước ngoài cư trú, hoạt động trên địa bàn tỉnh Điện Biên", "Quyết định", "UBND Tỉnh Điện Biên", "quoc-phong-an-ninh/an-ninh-trat-tu"},
	{"Quyết định số 63/2003/QĐ-UB V/v Quy định định mức chi phí giao đất và cấp giấy chứng nhận quyền sử dụng đất lâm nghiệp", "Quyết định", "UBND tỉnh Quảng Bình", "dat-dai/dang-ky-dat-dai"},
	{"Quyết định số 35/2020/QĐ-UBND Ban hành Quy chế tổ chức tuyển dụng công chức xã, phường, thị trấn trên địa bàn tỉnh Lâm Đồng", "Quyết định", "UBND Tỉnh Lâm Đồng", "bo-may-nha-nuoc/can-bo-cong-chuc"},
	{"Lệnh số 29/2015/L-CTN Về việc công bố Nghị quyết của Quốc hội", "Lệnh", "Chủ tịch nước", "tu-phap-dan-su/xay-dung-phap-luat"},
	{"Quyết định số 2851/2015/QĐ-UBND Về việc ban hành Quy định về điều kiện, tiêu chuẩn, chức danh đối với lãnh đạo, quản lý các phòng, các đơn vị sự nghiệp thuộc Sở Lao động Thương binh và Xã hội", "Quyết định", "UBND Thành phố Hải Phòng", "bo-may-nha-nuoc/can-bo-cong-chuc"},
	{"Quyết định số 15/2024/QĐ-UBND Sửa đổi, bổ sung một số điều của Quy định phân cấp thẩm quyền quản lý tổ chức bộ máy, biên chế, tuyển dụng, sử dụng và quản lý công chức, viên chức trên địa bàn tỉnh Tiền Giang", "Quyết định", "UBND tỉnh Tiền Giang", "bo-may-nha-nuoc/can-bo-cong-chuc"},
	{"Quyết định số 51/2015/QĐ-UBND Ban hành quy chế bán đấu giá tài sản nhà nước trên địa bàn tỉnh Lâm Đồng", "Quyết định", "UBND Tỉnh Lâm Đồng", "tai-chinh-cong/tai-san-cong"},
	{"Quyết định số 2894/2015/QĐ-UBND Quy định chế độ công tác phí, chế độ chi tổ chức các hội nghị đối với các cơ quan nhà nước và đơn vị sự nghiệp công lập tỉnh Thanh Hóa", "Quyết định", "UBND Tỉnh Thanh Hóa", "tai-chinh-cong/dinh-muc-chi-tieu"},
	{"Quyết định số 37/2003/QĐ-UB Về việc thực hiện chính sách tăng cường giáo viên cho các huyện vùng cao", "Quyết định", "UBND Tỉnh Nghệ An", "giao-duc/nha-giao"},
	{"Quyết định số 12/2003/QĐ-UB Về việc ban hành Quy chế làm việc của ban chỉ đạo cấp tỉnh các dự án MAG Quảng Bình", "Quyết định", "UBND tỉnh Quảng Bình", "bo-may-nha-nuoc/to-chuc-co-quan"},
	{"Quyết định số 17/2017/QĐ-UBND Quy chế quản lý và khai thác công trình kè trên địa bàn tỉnh An Giang", "Quyết định", "UBND Tỉnh An Giang", "nong-nghiep/thuy-loi-thien-tai"},
	{"Quyết định số 362/1998/QĐ-BTC Về việc triển khai thí điểm 2 loại hình bảo hiểm nhân thọ và áp dụng Bảo hiểm thương tật vĩnh viễn do tai nạn", "Quyết định", "Bộ Tài chính", "ngan-hang-tai-chinh/kinh-doanh-bao-hiem"},
	{"Quyết định 43/2026/QĐ-UBND Phân cấp thẩm quyền thực hiện một số nhiệm vụ quản lý nhà nước về người lao động Việt Nam đi làm việc ở nước ngoài theo hợp đồng trên địa bàn tỉnh Quảng Ninh", "Quyết định", "UBND Tỉnh Quảng Ninh", "lao-dong/lao-dong-nuoc-ngoai"},
	{"Quyết định số 62/2003/QĐ-UB về việc ban hành quy định tạm thời về chính sách tái định cư khi Nhà nước thu hồi đất sử dụng vào mục đích quốc phòng, an ninh, lợi ích quốc gia, lợi ích công cộng", "Quyết định", "UBND Thành phố Cần Thơ", "dat-dai/boi-thuong-tai-dinh-cu"},
	{"Thông tư số 55/2025/TT-NHNN Quy định cấp đổi Giấy phép, cấp bổ sung nội dung hoạt động vào Giấy phép và tổ chức, hoạt động của Tổ chức tín dụng phi ngân hàng", "Thông tư", "Ngân hàng Nhà nước Việt Nam", "ngan-hang-tai-chinh/to-chuc-tin-dung"},
	{"Quyết định số 51/2015/QĐ-UBND Về việc ban hành Kế hoạch phát triển nhà ở xã hội trên địa bàn tỉnh giai đoạn 2016 - 2020", "Quyết định", "UBND TỈNH BÌNH ĐỊNH", "xay-dung-do-thi/nha-o-bat-dong-san"},
	{"Quyết định số 19/2020/QĐ-UBND Ban hành Quy định việc công nhận các danh hiệu văn hóa trong Phong trào Toàn dân đoàn kết xây dựng đời sống văn hóa tỉnh An Giang", "Quyết định", "UBND Tỉnh An Giang", "van-hoa-the-thao-du-lich/gia-dinh-nep-song"},
	{"Thông tư số 24/2019/TT-BCT Sửa đổi, bổ sung một số điều của Thông tư số 45/2018/TT-BCT quy định vận hành thị trường bán buôn điện cạnh tranh và quy định phương pháp xác định giá phát điện, trình tự kiểm tra hợp đồng mua bán điện", "Thông tư", "Bộ Công Thương", "cong-nghiep-nang-luong/dien-luc"},
	{"Quyết định số 40/1998/QĐ-UB Về việc bãi bỏ Quyết định số 174/QĐ-UB ngày 21/8/1997", "Quyết định", "UBND Tỉnh Lào Cai", "tu-phap-dan-su/xay-dung-phap-luat"},
	{"Thông tư số 28/2015/TT-BTTTT Quy định danh mục vùng có điều kiện địa lý đặc biệt áp dụng tần suất thu gom và phát đặc thù trong cung ứng dịch vụ bưu chính công ích", "Thông tư", "Bộ Thông tin và Truyền thông", "thong-tin-truyen-thong/buu-chinh"},
	{"Quyết định số 38/2024/QĐ-UBND Ban hành quy định bảo đảm yêu cầu phòng, chống thiên tai đối với việc quản lý, vận hành, sử dụng công trình trên địa bàn tỉnh Bà Rịa Vũng Tàu", "Quyết định", "UBND tỉnh Bà Rịa - Vũng Tàu", "nong-nghiep/thuy-loi-thien-tai"},
	{"Nghị quyết liên tịch số 04/1985/NQLT Giữa Ban Bí thư Trung ương Đoàn TNCS Hồ Chí Minh và Bộ Tư pháp về việc tăng cường giáo dục pháp luật trong đoàn thanh niên và thanh niên", "Nghị quyết liên tịch", "Trung ương Đoàn thanh niên cộng sản Hồ Chí Minh, Bộ Tư pháp", "tu-phap-dan-su/xay-dung-phap-luat"},
	{"Quyết định số 19/2015/QĐ-UBND Ban hành Quy định chức năng, nhiệm vụ, quyền hạn và cơ cấu tổ chức của Sở Tư pháp tỉnh Bình Phước", "Quyết định", "UBND tỉnh Bình Phước", "bo-may-nha-nuoc/to-chuc-co-quan"},
	{"Quyết định số 373/2003/QĐ-NHNN Về việc sửa đổi, bổ sung một số điều của Quyết định số 440/2001/QĐ-NHNN về việc cho vay đối với người lao động đi làm việc có thời hạn ở nước ngoài", "Quyết định", "Ngân hàng Nhà nước Việt Nam", "ngan-hang-tai-chinh/tin-dung-lai-suat"},
	{"Nghị quyết số 05/2021/NQ-HĐND Ban hành quy định một số nội dung chi, mức chi phục vụ công tác bảo đảm trật tự an toàn giao thông trên địa bàn tỉnh Quảng Bình", "Nghị quyết", "HĐND tỉnh Quảng Bình", "giao-thong-van-tai/an-toan-giao-thong"},
}

// The gold set is what the classifier is actually judged on. The bar is set at
// the level the method can honestly reach: a lexical matcher over titles gets
// most of them and will never get all of them, and a bar it always clears
// measures nothing.
const goldFloor = 0.85

func TestClassifyAgainstTheGoldSet(t *testing.T) {
	v := MustLoad()
	if v.Get(gold[0].want) == nil {
		t.Fatalf("the gold set names subjects the vocabulary does not have")
	}
	right := 0
	for _, g := range gold {
		if v.Get(g.want) == nil {
			t.Errorf("gold entry names unknown subject %s", g.want)
			continue
		}
		doc := &law.Document{Title: g.title, DocType: g.docType, IssuingBody: g.body}
		got := v.Classify(doc)
		if hasSubject(got, g.want) {
			right++
			continue
		}
		t.Logf("missed %s for %q, got %s", g.want, g.title, names(got))
	}
	accuracy := float64(right) / float64(len(gold))
	if accuracy < goldFloor {
		t.Errorf("recall on the gold set is %.2f over %d documents, want at least %.2f", accuracy, len(gold), goldFloor)
	}
	t.Logf("gold set recall %.2f over %d documents", accuracy, len(gold))
}

func hasSubject(as []Assignment, id string) bool {
	for _, a := range as {
		if a.SubjectID == id {
			return true
		}
	}
	return false
}

func names(as []Assignment) string {
	var out []string
	for _, a := range as {
		out = append(out, a.SubjectID)
	}
	return strings.Join(out, ", ")
}

func TestClassifyCarriesTheDomainOfEverySubdomain(t *testing.T) {
	v := MustLoad()
	doc := &law.Document{Title: "Thông tư quy định về đăng ký lưu hành thuốc", DocType: "thông tư", IssuingBody: "Bộ Y tế"}
	got := v.Classify(doc)
	if !hasSubject(got, "y-te/duoc-my-pham") {
		t.Fatalf("got %s, want the pharmaceuticals subdomain", names(got))
	}
	if !hasSubject(got, "y-te") {
		t.Errorf("got %s, want the health domain carried up", names(got))
	}
}

func TestClassifyIsMultiLabelButBounded(t *testing.T) {
	v := MustLoad()
	doc := &law.Document{
		Title:       "Quyết định ban hành Quy chế phối hợp quản lý đất đai, xây dựng, môi trường, giao thông, y tế, giáo dục, thuế và lao động trên địa bàn",
		DocType:     "quyết định",
		IssuingBody: "UBND tỉnh Long An",
	}
	got := v.Classify(doc)
	subdomains := 0
	for _, a := range got {
		if strings.Contains(a.SubjectID, "/") {
			subdomains++
		}
	}
	if subdomains == 0 {
		t.Fatalf("a title naming eight fields matched nothing")
	}
	if subdomains > maxSubdomains {
		t.Errorf("got %d subdomains, want at most %d", subdomains, maxSubdomains)
	}
}

func TestClassifyLeavesAnUnreadableTitleUnassigned(t *testing.T) {
	v := MustLoad()
	doc := &law.Document{Title: "Quyết định về việc ban hành Quy chế làm việc", DocType: "quyết định"}
	got := v.Classify(doc)
	for _, a := range got {
		if strings.Contains(a.SubjectID, "/") && a.SubjectID != "bo-may-nha-nuoc/to-chuc-co-quan" {
			t.Errorf("a title saying nothing was filed under %s", a.SubjectID)
		}
	}
}

func TestClassifyRecordsWhatItMatchedOn(t *testing.T) {
	v := MustLoad()
	doc := &law.Document{Title: "Nghị định quy định chi tiết Luật an toàn thực phẩm", DocType: "nghị định"}
	for _, a := range v.Classify(doc) {
		if a.Method == MethodLexical && len(a.Matched) == 0 {
			t.Errorf("assignment to %s claims a lexical match and names no cue", a.SubjectID)
		}
		if a.Confidence <= 0 || a.Confidence >= 1 {
			t.Errorf("assignment to %s has confidence %v, want a number strictly between nought and one", a.SubjectID, a.Confidence)
		}
	}
}

func TestFoldMatchesWholeSyllablesOnly(t *testing.T) {
	hay := fold("Quyết định ban hành quy định về chất thải rắn")
	for _, inside := range []string{"cha", "an", "ra"} {
		if strings.Contains(hay, fold(inside)) {
			t.Errorf("%q matched inside a syllable of %q", inside, hay)
		}
	}
	if !strings.Contains(hay, fold("chất thải rắn")) {
		t.Errorf("a whole phrase failed to match itself")
	}
	// Punctuation in a cue has to survive the fold, because the corpus writes
	// these phrases with commas and the vocabulary copies them as written.
	if !strings.Contains(fold("phòng, chống thiên tai và tìm kiếm cứu nạn"), fold("phòng, chống thiên tai")) {
		t.Errorf("a comma broke the match")
	}
}

func TestPrimaryPrefersTheSubdomain(t *testing.T) {
	r := Record{Subjects: []Assignment{
		{SubjectID: "y-te", Method: MethodLexical},
		{SubjectID: "y-te/duoc-my-pham", Method: MethodLexical},
	}}
	if got := Primary(&r); got != "y-te/duoc-my-pham" {
		t.Errorf("got %q, want the subdomain", got)
	}
	r = Record{Subjects: []Assignment{{SubjectID: "y-te", Method: MethodLexical}}}
	if got := Primary(&r); got != "y-te" {
		t.Errorf("got %q, want the domain", got)
	}
	if got := Primary(&Record{}); got != "" {
		t.Errorf("got %q, want nothing for a document under nothing", got)
	}
}
